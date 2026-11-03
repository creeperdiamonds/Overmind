package skintest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// FakeMojang stands in for api.minecraftservices.com (the skin upload) and
// sessionserver.mojang.com (the signed profile). Nothing in the tests reaches
// Mojang. Fields are read and set under Mu.
type FakeMojang struct {
	srv *httptest.Server

	Mu        sync.Mutex
	Uploads   [][]byte // the PNGs uploaded, in order
	Variants  []string
	Tokens    []string // the Authorization header of each upload
	Times     []time.Time
	Stale     int    // session reads that still show the old skin
	Limit     int    // uploads still refused with a 429
	Retry     string // the Retry-After those carry
	Fail      int    // uploads still refused with a 400
	Refuse401 int    // uploads still refused with a 401
	current   string // the account's skin texture ID

	inFlight  atomic.Int32
	MaxFlight atomic.Int32 // the most uploads under way at once
}

// FakeAccount is the uploading account's UUID, without dashes.
const FakeAccount = "6f3a9e1c2b7d4e8fa0b1c2d3e4f5a6b7"

// NewFakeMojang starts the fake services; they stop when the test ends.
func NewFakeMojang(t testing.TB) *FakeMojang {
	m := &FakeMojang{current: "oldtexture"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /minecraft/profile/skins", func(w http.ResponseWriter, r *http.Request) {
		n := m.inFlight.Add(1)
		defer m.inFlight.Add(-1)
		for {
			mx := m.MaxFlight.Load()
			if n <= mx || m.MaxFlight.CompareAndSwap(mx, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		m.Mu.Lock()
		defer m.Mu.Unlock()
		m.Times = append(m.Times, time.Now())
		m.Tokens = append(m.Tokens, r.Header.Get("Authorization"))
		switch {
		case m.Refuse401 > 0:
			m.Refuse401--
			w.WriteHeader(http.StatusUnauthorized)
			return
		case m.Limit > 0:
			m.Limit--
			if m.Retry != "" {
				w.Header().Set("Retry-After", m.Retry)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			return
		case m.Fail > 0:
			m.Fail--
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(f)
		m.Uploads = append(m.Uploads, data)
		m.Variants = append(m.Variants, r.FormValue("variant"))
		m.current = fmt.Sprintf("tex%d", len(m.Uploads))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": FakeAccount, "name": "Uploader",
			"skins": []map[string]any{{"id": "x", "state": "ACTIVE", "url": "http://textures.minecraft.net/texture/" + m.current, "variant": "CLASSIC"}}})
	})
	mux.HandleFunc("GET /session/minecraft/profile/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.Mu.Lock()
		defer m.Mu.Unlock()
		if r.PathValue("id") != FakeAccount || r.URL.Query().Get("unsigned") != "false" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tex := m.current
		if m.Stale > 0 {
			m.Stale--
			tex = "oldtexture"
		}
		value := base64.StdEncoding.EncodeToString([]byte(`{"textures":{"SKIN":{"url":"http://textures.minecraft.net/texture/` + tex + `"}}}`))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": FakeAccount, "properties": []map[string]any{{"name": "textures", "value": value, "signature": "sig-" + tex}}})
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// URL is the fake services' address, for both ServicesURL and SessionURL.
func (m *FakeMojang) URL() string { return m.srv.URL }

// UploadCount is how many skins were uploaded.
func (m *FakeMojang) UploadCount() int {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return len(m.Uploads)
}
