package javafront

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/png" // skins are PNG
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/overmind/overmind/internal/skins"
)

// Skins between the editions.
//
// A Java player's skin is its Mojang profile's: the login's "textures"
// property, base64 JSON whose textures.SKIN.url is the PNG and whose
// metadata.model is "slim" for the slim (Alex) arms (minecraft.wiki "Mojang
// API" oldid=3782642). The PNG is fetched from textures.minecraft.net only,
// kept in memory (skinCacheSize skins, the oldest dropped first), and given to
// Bedrock as a Dragonfly skin: 64x64 or the legacy 64x32 RGBA (minecraft.wiki
// "Skin" oldid=3874470 says both are accepted), on Bedrock's built-in
// geometry.humanoid.custom or geometry.humanoid.customSlim (the names in
// gophertunnel 1.63.0's skin_resource_patch.json and skin_geometry.json). The
// player joins with the stand-in at once and gets its skin when the fetch is
// done, so a slow or failed fetch never holds up a join.
//
// Without a skin (an offline login, a profile with no custom skin, a fetch
// that failed) Bedrock sees the stand-in on the model the profile names.
// Mojang's own default skins are Mojang's images, so none is shipped here.
//
// A Bedrock player has no Mojang profile, and a Java client takes a textures
// property only if Mojang's key signed it ("A player property is considered
// valid by the client if signed by any of these keys", the same page). An
// unsigned property, or one with a made-up signature, is not valid, so Java
// players see a Bedrock player in the Java client's own default skin: no
// textures property is sent. Showing a Bedrock skin on Java would need the
// skin uploaded to a Mojang account to be signed (what GeyserMC's Global API
// does through MineSkin), which is an outside service and not done.

const (
	skinCacheSize = 128     // about 2 MB of 64x64 RGBA at most
	maxSkinPNG    = 1 << 16 // a skin PNG is a few KB
	skinTimeout   = 5 * time.Second
	skinHost      = "textures.minecraft.net"
)

// skinGeometry are Bedrock's built-in player models, by arm width.
const (
	geometryWide = "geometry.humanoid.custom"
	geometrySlim = "geometry.humanoid.customSlim"
)

// profileSkin is what a textures property says of the skin.
type profileSkin struct {
	URL  string
	Slim bool
}

// skinOf reads a login's textures property; ok is false when it names no skin.
func skinOf(textures string) (s profileSkin, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(textures)
	if err != nil {
		return s, false
	}
	var v struct {
		Textures struct {
			Skin *struct {
				URL      string `json:"url"`
				Metadata struct {
					Model string `json:"model"`
				} `json:"metadata"`
			} `json:"SKIN"`
		} `json:"textures"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Textures.Skin == nil || v.Textures.Skin.URL == "" {
		return s, false
	}
	return profileSkin{URL: v.Textures.Skin.URL, Slim: v.Textures.Skin.Metadata.Model == "slim"}, true
}

// skinFetcher fetches a skin PNG.
type skinFetcher func(ctx context.Context, url string) ([]byte, error)

var errSkinHost = errors.New("javafront: a skin is fetched from textures.minecraft.net only")

// fetchMojangSkin fetches a skin from textures.minecraft.net, over HTTPS
// whatever the property says, and no more than maxSkinPNG bytes of it.
func fetchMojangSkin(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != skinHost || u.User != nil || u.Port() != "" {
		return nil, errSkinHost
	}
	u.Scheme = "https"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := skinClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("javafront: skin fetch: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSkinPNG+1))
}

// skinClient follows a redirect only to textures.minecraft.net over HTTPS, so a
// skin URL cannot be bounced elsewhere.
var skinClient = &http.Client{
	Timeout: skinTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Hostname() != skinHost || req.URL.User != nil || req.URL.Port() != "" || len(via) > 3 {
			return errSkinHost
		}
		return nil
	},
}

// decodeSkin turns a skin PNG into a Bedrock skin. Only 64x64 and 64x32 are
// skins. A legacy 64x32 skin is made 64x64, its left arm and leg the right
// ones (the legacy layout has only those: minecraft.wiki "Skin"
// oldid=3874470), since Bedrock's built-in player models are 64x64
// (gophertunnel 1.63.0's skin_geometry.json has no 64x32 player model).
func decodeSkin(png []byte, slim bool) (skin.Skin, error) {
	if len(png) > maxSkinPNG {
		return skin.Skin{}, errors.New("javafront: skin too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return skin.Skin{}, err
	}
	if cfg.Width != 64 || cfg.Height != 64 && cfg.Height != 32 {
		return skin.Skin{}, fmt.Errorf("javafront: a skin is 64x64 or 64x32, not %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return skin.Skin{}, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	s := skin.New(64, 64)
	copy(s.Pix, rgba.Pix)
	if cfg.Height == 32 {
		// Left leg (16, 48) from the right leg (0, 16), left arm (32, 48)
		// from the right arm (40, 16): each 16 by 16.
		copyRect(s.Pix, 0, 16, 16, 48)
		copyRect(s.Pix, 40, 16, 32, 48)
	}
	s.ModelConfig = skin.ModelConfig{Default: geometryFor(slim)}
	return s, nil
}

func geometryFor(slim bool) string {
	if slim {
		return geometrySlim
	}
	return geometryWide
}

// skinCache keeps the skins fetched, by URL, and fetches each once at a time.
type skinCache struct {
	fetch skinFetcher

	mu      sync.Mutex
	skins   map[string]skin.Skin
	order   []string // oldest first
	pending map[string][]func(skin.Skin, bool)
}

func newSkinCache(fetch skinFetcher) *skinCache {
	if fetch == nil {
		fetch = fetchMojangSkin
	}
	return &skinCache{fetch: fetch, skins: map[string]skin.Skin{}, pending: map[string][]func(skin.Skin, bool){}}
}

// get calls done with the skin at url, at once when it is kept, else when a
// fetch is done (ok false if it failed). done runs on no particular goroutine.
func (sc *skinCache) get(p profileSkin, done func(skin.Skin, bool)) {
	sc.mu.Lock()
	if s, ok := sc.skins[p.URL]; ok {
		sc.mu.Unlock()
		done(s, true)
		return
	}
	waiting, busy := sc.pending[p.URL]
	sc.pending[p.URL] = append(waiting, done)
	sc.mu.Unlock()
	if busy {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), skinTimeout)
		defer cancel()
		var s skin.Skin
		png, err := sc.fetch(ctx, p.URL)
		if err == nil {
			s, err = decodeSkin(png, p.Slim)
		}
		sc.mu.Lock()
		calls := sc.pending[p.URL]
		delete(sc.pending, p.URL)
		if err == nil {
			sc.skins[p.URL] = s
			sc.order = append(sc.order, p.URL)
			for len(sc.order) > skinCacheSize {
				delete(sc.skins, sc.order[0])
				sc.order = sc.order[1:]
			}
		}
		sc.mu.Unlock()
		for _, f := range calls {
			f(s, err == nil)
		}
	}()
}

// len is how many skins are kept.
func (sc *skinCache) len() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.skins)
}

// joinSkin is the skin a Java player joins with: the stand-in, on the model
// its profile names.
func (c *conn) joinSkin() skin.Skin {
	p, _ := skinOf(c.id.Textures)
	return skins.StandIn(p.Slim)
}

// loadSkin fetches the player's Mojang skin, if it has one, and puts it on the
// player once it is there; Dragonfly then shows it to Bedrock viewers.
func (c *conn) loadSkin() {
	p, ok := skinOf(c.id.Textures)
	if !ok || c.f.skins == nil {
		return
	}
	c.f.skins.get(p, func(s skin.Skin, ok bool) {
		if !ok {
			c.f.opts.Log.Debug("java skin not fetched; the stand-in stays", "name", c.id.Name)
			return
		}
		player.Do(c.handle, func(_ *world.Tx, pl *player.Player) { pl.SetSkin(s) })
	})
}

// copyRect copies a 16 by 16 square of a 64-wide RGBA skin.
func copyRect(pix []uint8, fromX, fromY, toX, toY int) {
	for y := 0; y < 16; y++ {
		from := ((fromY+y)*64 + fromX) * 4
		to := ((toY+y)*64 + toX) * 4
		copy(pix[to:to+64], pix[from:from+64])
	}
}
