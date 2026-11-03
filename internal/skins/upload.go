package skins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The uploader: a Java-format Bedrock skin that is not a default skin reaches
// Java viewers only as a textures property Mojang signed, so it is set as the
// skin of a Java account the owner configured, the same call the launcher
// makes (POST api.minecraftservices.com/minecraft/profile/skins, multipart
// "variant" classic or slim and "file" the PNG, Bearer token; minecraft.wiki
// "Mojang API" oldid=3782642, "Upload skin"), and the account's signed
// textures property is read back from the session server
// (sessionserver.mojang.com/session/minecraft/profile/<uuid>?unsigned=false,
// the same page). The property stays valid after the account's skin changes
// again: it names the texture's own URL, signed with a timestamp.
//
// One upload at a time, at least Gap apart. A 429 waits for its Retry-After, or
// twice the last wait (1 minute at first, 30 at most), and the skin waits at
// the front of the queue: minecraft.wiki warns that many 429s on skin uploads
// can suspend the account. Each signed result is kept on disk for good, keyed
// by the image hash and arm width, so a skin is uploaded once ever. A skin that
// fails three times is not tried again until the server restarts. Tokens are
// never logged.

// Signed is a textures property and Mojang's signature of it.
type Signed struct {
	Value     string `json:"value"`
	Signature string `json:"signature"`
}

// TokenSource gives Minecraft services access tokens for the uploading
// account. Invalidate drops a token the services refused.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

// UploaderConfig configures an Uploader.
type UploaderConfig struct {
	Tokens TokenSource
	// CacheDir keeps the signed results, a file per skin.
	CacheDir string
	// Gap is the least time between two uploads; 0 is 30 seconds.
	Gap time.Duration
	// ServicesURL and SessionURL are Mojang's; empty uses
	// https://api.minecraftservices.com and https://sessionserver.mojang.com.
	ServicesURL, SessionURL string
	// PollEvery and PollTries pace reading the signed property back while the
	// session server still has the old skin; 0 is 5 seconds and 12 tries.
	PollEvery time.Duration
	PollTries int
	// BackoffStart is the first wait after a 429 with no Retry-After; 0 is 1
	// minute.
	BackoffStart time.Duration
	HTTP         *http.Client
	Log          *slog.Logger
}

const (
	maxBackoff  = 30 * time.Minute
	maxAttempts = 3
)

// Uploader uploads skins one at a time and keeps what Mojang signed.
type Uploader struct {
	cfg UploaderConfig

	mu      sync.Mutex
	signed  map[string]Signed
	queue   []*job
	queued  map[string]*job
	failed  map[string]bool
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	backoff time.Duration
}

type job struct {
	key      string
	png      []byte
	slim     bool
	attempts int
	waiters  []func(Signed)
}

// NewUploader starts an uploader, reading what earlier runs had signed.
func NewUploader(cfg UploaderConfig) (*Uploader, error) {
	if cfg.Tokens == nil {
		return nil, errors.New("skins: an uploader needs an account")
	}
	if cfg.Gap <= 0 {
		cfg.Gap = 30 * time.Second
	}
	if cfg.ServicesURL == "" {
		cfg.ServicesURL = "https://api.minecraftservices.com"
	}
	if cfg.SessionURL == "" {
		cfg.SessionURL = "https://sessionserver.mojang.com"
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 5 * time.Second
	}
	if cfg.PollTries <= 0 {
		cfg.PollTries = 12
	}
	if cfg.BackoffStart <= 0 {
		cfg.BackoffStart = time.Minute
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("skins: cache folder: %w", err)
	}
	u := &Uploader{cfg: cfg, signed: map[string]Signed{}, queued: map[string]*job{}, failed: map[string]bool{},
		wake: make(chan struct{}, 1), done: make(chan struct{})}
	entries, err := os.ReadDir(cfg.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("skins: cache folder: %w", err)
	}
	for _, e := range entries {
		key, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !keyPattern.MatchString(key) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cfg.CacheDir, e.Name()))
		var s Signed
		if err != nil || json.Unmarshal(data, &s) != nil || s.Value == "" || s.Signature == "" {
			cfg.Log.Warn("skin cache entry unreadable; it will be uploaded again", "file", e.Name())
			continue
		}
		u.signed[key] = s
	}
	u.ctx, u.cancel = context.WithCancel(context.Background())
	go u.run()
	return u, nil
}

var keyPattern = regexp.MustCompile(`^[0-9a-f]{64}-(slim|wide)$`)

// Key is the cache key of an image hash on a model.
func Key(hash string, slim bool) string {
	if slim {
		return hash + "-slim"
	}
	return hash + "-wide"
}

// Signed gives the signed textures of a skin, if it has been uploaded.
func (u *Uploader) Signed(hash string, slim bool) (Signed, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s, ok := u.signed[Key(hash, slim)]
	return s, ok
}

// Request queues a skin (its 64x64 RGBA pixels and hash) to be uploaded, once
// however often it is asked, and calls done when it is signed, on the
// uploader's goroutine. It does nothing for a skin already signed (done is
// called at once) or one that failed.
func (u *Uploader) Request(hash string, pix []byte, slim bool, done func(Signed)) {
	key := Key(hash, slim)
	u.mu.Lock()
	if s, ok := u.signed[key]; ok {
		u.mu.Unlock()
		if done != nil {
			done(s)
		}
		return
	}
	if u.failed[key] {
		u.mu.Unlock()
		return
	}
	if j, ok := u.queued[key]; ok {
		if done != nil {
			j.waiters = append(j.waiters, done)
		}
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	png, err := PNG(pix)
	if err != nil {
		return
	}
	u.mu.Lock()
	if _, ok := u.queued[key]; !ok {
		j := &job{key: key, png: png, slim: slim}
		if done != nil {
			j.waiters = append(j.waiters, done)
		}
		u.queued[key] = j
		u.queue = append(u.queue, j)
	} else if done != nil {
		u.queued[key].waiters = append(u.queued[key].waiters, done)
	}
	u.mu.Unlock()
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// Pending is how many skins wait to be uploaded.
func (u *Uploader) Pending() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.queue)
}

// Close stops uploading. A skin under way is given up; it is tried again next
// run.
func (u *Uploader) Close() {
	u.cancel()
	<-u.done
}

func (u *Uploader) run() {
	defer close(u.done)
	var last time.Time
	for {
		u.mu.Lock()
		var j *job
		if len(u.queue) > 0 {
			j = u.queue[0]
		}
		u.mu.Unlock()
		if j == nil {
			select {
			case <-u.ctx.Done():
				return
			case <-u.wake:
				continue
			}
		}
		if wait := time.Until(last.Add(u.cfg.Gap)); !last.IsZero() && wait > 0 {
			if !u.sleep(wait) {
				return
			}
		}
		last = time.Now()
		s, err := u.upload(j)
		var limited *rateLimited
		switch {
		case u.ctx.Err() != nil:
			return
		case errors.As(err, &limited):
			wait := limited.retryAfter
			if wait <= 0 {
				wait = max(u.cfg.BackoffStart, u.backoff*2)
				wait = min(wait, maxBackoff)
			}
			u.backoff = wait
			u.cfg.Log.Warn("skin upload rate limited; waiting", "wait", wait)
			if !u.sleep(wait) {
				return
			}
			continue // the same skin, still first
		case err != nil:
			u.backoff = 0
			j.attempts++
			u.cfg.Log.Warn("skin upload failed", "skin", j.key, "attempt", j.attempts, "err", err)
			u.mu.Lock()
			u.queue = u.queue[1:]
			if j.attempts >= maxAttempts {
				delete(u.queued, j.key)
				u.failed[j.key] = true
			} else {
				u.queue = append(u.queue, j)
			}
			u.mu.Unlock()
			continue
		}
		u.backoff = 0
		if err := u.store(j.key, s); err != nil {
			u.cfg.Log.Error("skin cache not written; the skin is kept for this run only", "skin", j.key, "err", err)
		}
		u.mu.Lock()
		u.signed[j.key] = s
		u.queue = u.queue[1:]
		delete(u.queued, j.key)
		waiters := j.waiters
		u.mu.Unlock()
		u.cfg.Log.Info("skin uploaded and signed", "skin", j.key)
		for _, f := range waiters {
			f(s)
		}
	}
}

func (u *Uploader) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-u.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// store writes a signed result atomically: a temporary file, then a rename.
func (u *Uploader) store(key string, s Signed) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(u.cfg.CacheDir, key+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(u.cfg.CacheDir, key+".json"))
}

type rateLimited struct{ retryAfter time.Duration }

func (r *rateLimited) Error() string { return "skins: rate limited (429)" }

// upload sets the account's skin and reads back the signed property naming it.
func (u *Uploader) upload(j *job) (Signed, error) {
	profile, err := u.setSkin(j, true)
	if err != nil {
		return Signed{}, err
	}
	for try := range u.cfg.PollTries {
		if try > 0 && !u.sleep(u.cfg.PollEvery) {
			return Signed{}, u.ctx.Err()
		}
		s, url, err := u.readTextures(profile.id)
		if err != nil {
			var limited *rateLimited
			if errors.As(err, &limited) {
				return Signed{}, err
			}
			continue
		}
		if sameTexture(url, profile.url) {
			return s, nil
		}
	}
	return Signed{}, errors.New("skins: the session server never showed the new skin")
}

type uploaded struct{ id, url string }

func (u *Uploader) setSkin(j *job, retryAuth bool) (uploaded, error) {
	token, err := u.cfg.Tokens.Token(u.ctx)
	if err != nil {
		return uploaded{}, fmt.Errorf("skins: account token: %w", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	variant := "classic"
	if j.slim {
		variant = "slim"
	}
	_ = mw.WriteField("variant", variant)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="skin.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(j.png)
	_ = mw.Close()
	req, err := http.NewRequestWithContext(u.ctx, http.MethodPost, u.cfg.ServicesURL+"/minecraft/profile/skins", &body)
	if err != nil {
		return uploaded{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := u.cfg.HTTP.Do(req)
	if err != nil {
		return uploaded{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return uploaded{}, &rateLimited{retryAfter: retryAfter(resp)}
	case resp.StatusCode == http.StatusUnauthorized && retryAuth:
		u.cfg.Tokens.Invalidate()
		return u.setSkin(j, false)
	case resp.StatusCode/100 != 2:
		return uploaded{}, fmt.Errorf("skins: upload: %s", resp.Status)
	}
	var p struct {
		ID    string `json:"id"`
		Skins []struct {
			State string `json:"state"`
			URL   string `json:"url"`
		} `json:"skins"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.ID == "" {
		return uploaded{}, errors.New("skins: upload: no profile in the answer")
	}
	for _, s := range p.Skins {
		if s.State == "ACTIVE" {
			return uploaded{id: p.ID, url: s.URL}, nil
		}
	}
	return uploaded{}, errors.New("skins: upload: no active skin in the answer")
}

func retryAfter(resp *http.Response) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
		return min(time.Duration(s)*time.Second, maxBackoff)
	}
	return 0
}

// readTextures reads an account's signed textures property and the skin URL in
// it.
func (u *Uploader) readTextures(id string) (Signed, string, error) {
	req, err := http.NewRequestWithContext(u.ctx, http.MethodGet, u.cfg.SessionURL+"/session/minecraft/profile/"+strings.ReplaceAll(id, "-", "")+"?unsigned=false", nil)
	if err != nil {
		return Signed{}, "", err
	}
	resp, err := u.cfg.HTTP.Do(req)
	if err != nil {
		return Signed{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return Signed{}, "", &rateLimited{retryAfter: retryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		return Signed{}, "", fmt.Errorf("skins: session server: %s", resp.Status)
	}
	var p struct {
		Properties []struct {
			Name      string `json:"name"`
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return Signed{}, "", err
	}
	for _, pr := range p.Properties {
		if pr.Name != "textures" || pr.Signature == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(pr.Value)
		if err != nil {
			return Signed{}, "", err
		}
		var v struct {
			Textures struct {
				Skin struct {
					URL string `json:"url"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return Signed{}, "", errors.New("skins: unreadable textures property")
		}
		return Signed{Value: pr.Value, Signature: pr.Signature}, v.Textures.Skin.URL, nil
	}
	return Signed{}, "", errors.New("skins: no signed textures property")
}

// sameTexture compares two texture URLs by the texture they name (the last
// path element), whatever the scheme.
func sameTexture(a, b string) bool {
	last := func(s string) string { return s[strings.LastIndex(s, "/")+1:] }
	return a != "" && last(a) != "" && last(a) == last(b)
}
