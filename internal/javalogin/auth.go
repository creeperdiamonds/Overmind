package javalogin

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// DefaultSessionURL is the Mojang check that a player took part in the key
// exchange under their account.
const DefaultSessionURL = "https://sessionserver.mojang.com/session/minecraft/hasJoined"

// sessionTimeout bounds one call to the session server.
const sessionTimeout = 10 * time.Second

// maxProfileBody bounds the session server's answer.
const maxProfileBody = 64 << 10

var (
	errNotJoined = errors.New("session server did not know this player")
	errSession   = errors.New("session server unavailable")
)

// ServerHash is Minecraft's session hash: SHA-1 over the server ID, shared
// secret and public key, read as a signed big-endian number in hex.
func ServerHash(serverID string, secret, publicKey []byte) string {
	h := sha1.New()
	h.Write([]byte(serverID))
	h.Write(secret)
	h.Write(publicKey)
	sum := h.Sum(nil)
	n := new(big.Int).SetBytes(sum)
	if sum[0]&0x80 != 0 {
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(len(sum))*8))
	}
	return n.Text(16)
}

type profile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Properties []struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		Signature string `json:"signature"`
	} `json:"properties"`
}

// hasJoined asks the session server who joined with this hash.
func hasJoined(ctx context.Context, base, name, hash string) (*Identity, error) {
	if base == "" {
		base = DefaultSessionURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSession, err)
	}
	q := u.Query()
	q.Set("username", name)
	q.Set("serverId", hash)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, sessionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSession, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSession, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		return nil, errNotJoined
	default:
		return nil, fmt.Errorf("%w: status %d", errSession, resp.StatusCode)
	}
	var p profile
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProfileBody)).Decode(&p); err != nil {
		return nil, fmt.Errorf("%w: %v", errSession, err)
	}
	id, err := uuid.Parse(p.ID)
	if err != nil || !validName(p.Name) {
		return nil, fmt.Errorf("%w: bad profile", errSession)
	}
	out := &Identity{Name: p.Name, UUID: id}
	for _, pr := range p.Properties {
		if pr.Name == "textures" && len(pr.Value) <= 16<<10 {
			out.Textures = pr.Value
			if len(pr.Signature) <= 4<<10 {
				out.TexturesSignature = pr.Signature
			}
		}
	}
	return out, nil
}
