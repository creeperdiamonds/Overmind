package skins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/skins"
	"github.com/creeperdiamonds/overmind/internal/skins/skintest"
)

type fixedTokens struct {
	token       string
	invalidated atomic.Int32
}

func (f *fixedTokens) Token(context.Context) (string, error) {
	if f.invalidated.Load() > 0 {
		return f.token + "-new", nil
	}
	return f.token, nil
}
func (f *fixedTokens) Invalidate() { f.invalidated.Add(1) }

func newUploader(t *testing.T, m *skintest.FakeMojang, dir string, log *bytes.Buffer, gap time.Duration) (*skins.Uploader, *fixedTokens) {
	tok := &fixedTokens{token: "secret-token-123"}
	var out io.Writer = io.Discard
	if log != nil {
		out = log
	}
	u, err := skins.NewUploader(skins.UploaderConfig{Tokens: tok, CacheDir: dir, Gap: gap, ServicesURL: m.URL(), SessionURL: m.URL(),
		PollEvery: 10 * time.Millisecond, PollTries: 5, BackoffStart: 50 * time.Millisecond, Log: slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	require.NoError(t, err)
	t.Cleanup(u.Close)
	return u, tok
}

func wait(t *testing.T, ch <-chan skins.Signed) skins.Signed {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("no signed skin")
		return skins.Signed{}
	}
}

// A skin is uploaded as the launcher does, read back signed once the session
// server shows it, kept on disk, and never uploaded again, even by the next
// run. Many asking at once make one upload.
func TestUploadOnceEver(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	m.Stale = 2
	dir := t.TempDir()
	var log bytes.Buffer
	u, _ := newUploader(t, m, dir, &log, time.Millisecond)
	pix := skintest.Image(64, 64, 7)
	hash := skins.Hash(pix)
	got := make(chan skins.Signed, 10)
	for range 5 {
		u.Request(hash, pix, true, func(s skins.Signed) { got <- s })
	}
	for range 5 {
		s := wait(t, got)
		require.Equal(t, "sig-tex1", s.Signature, "the stale property was taken")
	}
	sg, ok := u.Signed(hash, true)
	require.True(t, ok)
	require.Equal(t, "sig-tex1", sg.Signature)
	_, ok = u.Signed(hash, false)
	require.False(t, ok, "the wide model is another skin")

	m.Mu.Lock()
	require.Len(t, m.Uploads, 1)
	require.Equal(t, "slim", m.Variants[0])
	require.Equal(t, "Bearer secret-token-123", m.Tokens[0])
	img, err := png.Decode(bytes.NewReader(m.Uploads[0]))
	require.NoError(t, err)
	require.Equal(t, 64, img.Bounds().Dx())
	r, g, b, a := img.At(3, 5).RGBA()
	i := (5*64 + 3) * 4
	require.Equal(t, []uint32{uint32(pix[i]), uint32(pix[i+1]), uint32(pix[i+2]), uint32(pix[i+3])}, []uint32{r >> 8, g >> 8, b >> 8, a >> 8})
	m.Mu.Unlock()

	_, err = os.Stat(filepath.Join(dir, skins.Key(hash, true)+".json"))
	require.NoError(t, err, "not kept on disk")
	u.Close()

	again, _ := newUploader(t, m, dir, nil, time.Millisecond)
	sg, ok = again.Signed(hash, true)
	require.True(t, ok, "the next run forgot it")
	require.Equal(t, "sig-tex1", sg.Signature)
	done := make(chan skins.Signed, 1)
	again.Request(hash, pix, true, func(s skins.Signed) { done <- s })
	require.Equal(t, "sig-tex1", wait(t, done).Signature)
	m.Mu.Lock()
	require.Len(t, m.Uploads, 1, "uploaded again")
	m.Mu.Unlock()
	require.NotContains(t, log.String(), "secret-token-123", "a token was logged")
}

// One upload at a time, at least the gap apart.
func TestUploadsOneAtATime(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	gap := 150 * time.Millisecond
	u, _ := newUploader(t, m, t.TempDir(), nil, gap)
	got := make(chan skins.Signed, 3)
	for i := range 3 {
		pix := skintest.Image(64, 64, byte(20+i))
		u.Request(skins.Hash(pix), pix, false, func(s skins.Signed) { got <- s })
	}
	for range 3 {
		wait(t, got)
	}
	require.EqualValues(t, 1, m.MaxFlight.Load())
	m.Mu.Lock()
	defer m.Mu.Unlock()
	require.Len(t, m.Times, 3)
	for i := 1; i < 3; i++ {
		require.GreaterOrEqual(t, m.Times[i].Sub(m.Times[i-1]), gap-10*time.Millisecond)
	}
}

// A 429 waits for its Retry-After, then the same skin goes again.
func TestUploadRateLimited(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	m.Limit, m.Retry = 1, "1"
	u, _ := newUploader(t, m, t.TempDir(), nil, time.Millisecond)
	pix := skintest.Image(64, 64, 30)
	got := make(chan skins.Signed, 1)
	start := time.Now()
	u.Request(skins.Hash(pix), pix, false, func(s skins.Signed) { got <- s })
	wait(t, got)
	require.GreaterOrEqual(t, time.Since(start), time.Second, "Retry-After not waited for")
	m.Mu.Lock()
	require.Len(t, m.Times, 2)
	m.Mu.Unlock()

	// Without Retry-After, the wait doubles.
	m.Mu.Lock()
	m.Limit, m.Retry = 3, ""
	m.Times = nil
	m.Mu.Unlock()
	pix = skintest.Image(64, 64, 31)
	u.Request(skins.Hash(pix), pix, false, func(s skins.Signed) { got <- s })
	wait(t, got)
	m.Mu.Lock()
	defer m.Mu.Unlock()
	require.Len(t, m.Times, 4)
	d1, d2, d3 := m.Times[1].Sub(m.Times[0]), m.Times[2].Sub(m.Times[1]), m.Times[3].Sub(m.Times[2])
	require.GreaterOrEqual(t, d1, 50*time.Millisecond)
	require.GreaterOrEqual(t, d2, 100*time.Millisecond)
	require.GreaterOrEqual(t, d3, 200*time.Millisecond)
}

// A skin refused three times is given up; a refused token is made again once.
func TestUploadFailures(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	m.Fail = 3
	u, _ := newUploader(t, m, t.TempDir(), nil, time.Millisecond)
	pix := skintest.Image(64, 64, 40)
	called := atomic.Bool{}
	u.Request(skins.Hash(pix), pix, false, func(skins.Signed) { called.Store(true) })
	require.Eventually(t, func() bool { return u.Pending() == 0 }, 5*time.Second, 10*time.Millisecond)
	require.False(t, called.Load())
	_, ok := u.Signed(skins.Hash(pix), false)
	require.False(t, ok)
	u.Request(skins.Hash(pix), pix, false, nil)
	require.Equal(t, 0, u.Pending(), "a failed skin was queued again")

	m.Mu.Lock()
	m.Refuse401 = 1
	m.Tokens = nil
	m.Mu.Unlock()
	pix = skintest.Image(64, 64, 41)
	got := make(chan skins.Signed, 1)
	u.Request(skins.Hash(pix), pix, false, func(s skins.Signed) { got <- s })
	wait(t, got)
	m.Mu.Lock()
	defer m.Mu.Unlock()
	require.Equal(t, []string{"Bearer secret-token-123", "Bearer secret-token-123-new"}, m.Tokens)
}

// A broken cache file is read past, and the skin uploaded again.
func TestUploadCacheBroken(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	dir := t.TempDir()
	pix := skintest.Image(64, 64, 50)
	require.NoError(t, os.WriteFile(filepath.Join(dir, skins.Key(skins.Hash(pix), false)+".json"), []byte("{not json"), 0o644))
	u, _ := newUploader(t, m, dir, nil, time.Millisecond)
	_, ok := u.Signed(skins.Hash(pix), false)
	require.False(t, ok)
}

// The account chain against fake Microsoft, Xbox Live and Mojang services: a
// refreshed Live token, the Xbox user and XSTS tokens, login_with_xbox, and
// the new refresh token kept. Errors and logs carry no token.
func TestAccountTokens(t *testing.T) {
	var calls []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		note("live " + r.Form.Get("grant_type") + " " + r.Form.Get("refresh_token") + " " + r.Form.Get("scope"))
		if r.Form.Get("refresh_token") == "dead" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token dead"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"live-access","refresh_token":"refresh-2","expires_in":3600}`))
	})
	mux.HandleFunc("POST /user", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Properties struct{ RpsTicket string } }
		_ = json.NewDecoder(r.Body).Decode(&body)
		note("user " + body.Properties.RpsTicket)
		_, _ = w.Write([]byte(`{"Token":"user-token"}`))
	})
	mux.HandleFunc("POST /xsts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RelyingParty string
			Properties   struct{ UserTokens []string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		note("xsts " + body.RelyingParty + " " + strings.Join(body.Properties.UserTokens, ","))
		_, _ = w.Write([]byte(`{"Token":"xsts-token","DisplayClaims":{"xui":[{"uhs":"12345"}]}}`))
	})
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IdentityToken string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		note("login " + body.IdentityToken)
		_, _ = w.Write([]byte(`{"access_token":"mc-token","expires_in":86400}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ep := skins.Endpoints{LiveToken: srv.URL + "/token", UserAuth: srv.URL + "/user", XSTS: srv.URL + "/xsts", LoginWithXbox: srv.URL + "/login"}

	path := filepath.Join(t.TempDir(), "account.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"refresh_token":"refresh-1"}`), 0o600))
	acc, err := skins.OpenAccount(path, ep, nil)
	require.NoError(t, err)
	tok, err := acc.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "mc-token", tok)
	tok, err = acc.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, "mc-token", tok)
	require.Equal(t, []string{
		"live refresh_token refresh-1 service::user.auth.xboxlive.com::MBI_SSL",
		"user t=live-access",
		"xsts rp://api.minecraftservices.com/ user-token",
		"login XBL3.0 x=12345;xsts-token",
	}, calls, "a kept token was not used again")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "refresh-2", "the new refresh token was not kept")

	acc.Invalidate()
	_, err = acc.Token(context.Background())
	require.NoError(t, err)
	require.Contains(t, calls[4], "refresh-2")

	require.NoError(t, os.WriteFile(path, []byte(`{"refresh_token":"dead"}`), 0o600))
	acc.Invalidate()
	_, err = acc.Token(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "dead")
	require.NotContains(t, err.Error(), "token dead")
}

// The device code sign-in: it prints where to go and the code, waits while the
// owner signs in, then keeps the refresh token.
func TestDeviceLogin(t *testing.T) {
	polls := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device_code":"dev","user_code":"ABCD-EFGH","verification_uri":"https://www.microsoft.com/link","interval":1,"expires_in":30}`))
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("device_code") != "dev" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if polls.Add(1) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"the-refresh"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "acc", "account.json")
	var out bytes.Buffer
	require.NoError(t, skins.DeviceLogin(context.Background(), path, &out, skins.Endpoints{LiveConnect: srv.URL + "/connect", LiveToken: srv.URL + "/token"}, nil))
	require.Contains(t, out.String(), "https://www.microsoft.com/link")
	require.Contains(t, out.String(), "ABCD-EFGH")
	require.NotContains(t, out.String(), "the-refresh")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "the-refresh")
}
