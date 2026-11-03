package javalogin

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/javawire"
	"github.com/overmind/overmind/internal/tables/java261"
)

func hashOfName(name string) string { return ServerHash(name, nil, nil) }

func TestServerHashVectors(t *testing.T) {
	require.Equal(t, "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48", hashOfName("Notch"))
	require.Equal(t, "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1", hashOfName("jeb_"))
	require.Equal(t, "88e16a1019277b15d58faf0541e11910eb756f6", hashOfName("simon"))
}

// harness runs Accept on one end of a loopback connection and gives the
// test the other end.
type harness struct {
	t      *testing.T
	client *javawire.Conn
	raw    net.Conn
	done   chan result
}

type result struct {
	id  *Identity
	err error
}

func start(t *testing.T, opts Options) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	h := &harness{t: t, done: make(chan result, 1)}
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			h.done <- result{err: err}
			return
		}
		defer nc.Close()
		id, err := Accept(javawire.NewConn(nc), opts)
		h.done <- result{id, err}
	}()
	h.raw, err = net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { h.raw.Close() })
	h.client = javawire.NewConn(h.raw)
	require.NoError(t, h.raw.SetDeadline(time.Now().Add(40*time.Second)))
	return h
}

func (h *harness) send(w *javaproto.Writer) {
	h.t.Helper()
	require.NoError(h.t, h.client.WriteFrame(w.B))
}

func pkt(id int32) *javaproto.Writer { w := &javaproto.Writer{}; w.VarInt(id); return w }

func (h *harness) handshake(proto, next int32) {
	w := pkt(javaproto.HandshakeSetProtocol)
	w.VarInt(proto)
	w.String("localhost")
	w.B = append(w.B, 0x63, 0xdd)
	w.VarInt(next)
	h.send(w)
}

func (h *harness) loginStart(name string) {
	w := pkt(javaproto.LoginStart)
	w.String(name)
	w.UUID(uuid.Nil)
	h.send(w)
}

// recv returns the next packet's ID and a reader over its body.
func (h *harness) recv() (int32, *javaproto.Reader) {
	h.t.Helper()
	f, err := h.client.ReadFrame()
	require.NoError(h.t, err)
	r := javaproto.NewReader(append([]byte(nil), f...))
	return r.VarInt(), r
}

func (h *harness) result(d time.Duration) result {
	h.t.Helper()
	select {
	case r := <-h.done:
		return r
	case <-time.After(d):
		h.t.Fatal("Accept did not return")
		return result{}
	}
}

func TestStatusReportsVersion(t *testing.T) {
	h := start(t, Options{MOTD: "hello", MaxPlayers: 20, OnlinePlayers: func() int { return 3 }})
	h.handshake(java261.ProtocolVersion, javaproto.NextStatus)
	h.send(pkt(javaproto.StatusRequest))
	id, r := h.recv()
	require.Equal(t, javaproto.StatusResponse, id)
	var st struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct{ Max, Online int } `json:"players"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.String(32767)), &st))
	require.Equal(t, 775, st.Version.Protocol)
	require.Equal(t, "26.1", st.Version.Name)
	require.Equal(t, 20, st.Players.Max)
	require.Equal(t, 3, st.Players.Online)
}

func TestPingEchoes(t *testing.T) {
	h := start(t, Options{})
	h.handshake(java261.ProtocolVersion, javaproto.NextStatus)
	h.send(pkt(javaproto.StatusRequest))
	h.recv()
	w := pkt(javaproto.StatusPing)
	w.Long(0x1234567890abcdef)
	h.send(w)
	id, r := h.recv()
	require.Equal(t, javaproto.StatusPong, id)
	require.Equal(t, int64(0x1234567890abcdef), r.Long())
	require.ErrorIs(t, h.result(5*time.Second).err, ErrStatusServed)
}

func TestWrongProtocolIsToldVersion(t *testing.T) {
	h := start(t, Options{})
	h.handshake(777, javaproto.NextLogin)
	id, r := h.recv()
	require.Equal(t, javaproto.LoginDisconnect, id)
	require.Contains(t, r.String(32767), "26.1")
	require.Error(t, h.result(5*time.Second).err)
}

func TestTruncatedLoginDoesNotHang(t *testing.T) {
	h := start(t, Options{})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	// A frame that promises 30 bytes and delivers 3, then the client leaves.
	_, err := h.raw.Write([]byte{30, 0, 5, 'a'})
	require.NoError(t, err)
	h.raw.Close()
	require.Error(t, h.result(5*time.Second).err)
}

func TestHostileInputIsRejected(t *testing.T) {
	cases := map[string]func(h *harness){
		"long name": func(h *harness) {
			h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
			h.loginStart(strings.Repeat("a", 17))
		},
		"huge frame":      func(h *harness) { h.raw.Write([]byte{0xff, 0xff, 0xff, 0x7f}) },
		"bad next state":  func(h *harness) { h.handshake(java261.ProtocolVersion, 9) },
		"empty handshake": func(h *harness) { h.send(pkt(javaproto.HandshakeSetProtocol)) },
		"login before encryption reply": func(h *harness) {
			h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
			h.loginStart("Notch")
			h.send(pkt(javaproto.LoginAcknowledged))
		},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := start(t, Options{SessionURL: "http://127.0.0.1:1"})
			body(h)
			require.Error(t, h.result(5*time.Second).err)
		})
	}
}

// session fakes Mojang's hasJoined endpoint.
func fakeSession(t *testing.T, h http.HandlerFunc) string {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

// encrypt performs the client half of the encryption handshake and returns
// the secret and the serverId the server announced.
func (h *harness) encrypt() (secret, pub []byte, serverID string) {
	t := h.t
	id, r := h.recv()
	require.Equal(t, javaproto.LoginEncryptionReq, id)
	serverID = r.String(20)
	pub = r.Bytes(1024)
	token := r.Bytes(64)
	require.True(t, r.Bool(), "client must be told to authenticate")
	require.NoError(t, r.Err())
	k, err := x509.ParsePKIXPublicKey(pub)
	require.NoError(t, err)
	secret = make([]byte, 16)
	rand.Read(secret)
	es, err := rsa.EncryptPKCS1v15(rand.Reader, k.(*rsa.PublicKey), secret)
	require.NoError(t, err)
	et, err := rsa.EncryptPKCS1v15(rand.Reader, k.(*rsa.PublicKey), token)
	require.NoError(t, err)
	w := pkt(javaproto.LoginEncryptionResp)
	w.Bytes(es)
	w.Bytes(et)
	h.send(w)
	require.NoError(t, h.client.EnableEncryption(secret))
	return
}

func TestAuthRejectsWhenMojangSaysNo(t *testing.T) {
	var query atomic.Value
	surl := fakeSession(t, func(w http.ResponseWriter, r *http.Request) {
		query.Store(r.URL.Query())
		w.WriteHeader(http.StatusNoContent)
	})
	h := start(t, Options{SessionURL: surl})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Notch")
	secret, pub, serverID := h.encrypt()
	id, _ := h.recv()
	require.Equal(t, javaproto.LoginDisconnect, id)
	require.Error(t, h.result(5*time.Second).err)
	q := query.Load().(url.Values)
	require.Equal(t, "Notch", q["username"][0])
	require.Equal(t, ServerHash(serverID, secret, pub), q["serverId"][0])
}

func TestAuthAcceptsAndTakesProfileFromMojang(t *testing.T) {
	want := uuid.MustParse("069a79f4-44e9-4726-a5be-fca90e38aaf5")
	surl := fakeSession(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": strings.ReplaceAll(want.String(), "-", ""), "name": "Notch",
			"properties": []map[string]string{{"name": "textures", "value": "dGV4", "signature": "c2ln"}},
		})
	})
	h := start(t, Options{SessionURL: surl})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Notch")
	h.encrypt()
	id, r := h.recv()
	require.Equal(t, javaproto.LoginCompress, id)
	require.EqualValues(t, 256, r.VarInt())
	h.client.EnableCompression(256)
	id, r = h.recv()
	require.Equal(t, javaproto.LoginSuccess, id)
	require.Equal(t, want, r.UUID())
	require.Equal(t, "Notch", r.String(16))
	require.EqualValues(t, 1, r.VarInt())
	require.Equal(t, "textures", r.String(64))
	require.Equal(t, "dGV4", r.String(64))
	require.True(t, r.Bool(), "signature present")
	require.Equal(t, "c2ln", r.String(64))
	h.finishConfig(t)
	res := h.result(5 * time.Second)
	require.NoError(t, res.err)
	// No settings came, so every skin layer shows.
	require.Equal(t, Identity{Name: "Notch", UUID: want, Textures: "dGV4", TexturesSignature: "c2ln", SkinParts: AllSkinParts}, *res.id)
}

// The skin layers come from the client's settings in configuration.
func TestSettingsGiveSkinParts(t *testing.T) {
	h := start(t, Options{Offline: true})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Steve")
	id, _ := h.recv()
	require.Equal(t, javaproto.LoginCompress, id)
	h.client.EnableCompression(256)
	id, _ = h.recv()
	require.Equal(t, javaproto.LoginSuccess, id)
	w := pkt(javaproto.ConfigSettings)
	w.String("en_us")
	w.B = append(w.B, 8) // view distance
	w.VarInt(0)          // chat flags
	w.Bool(true)
	w.B = append(w.B, 0x15) // skin parts
	w.VarInt(1)             // main hand
	w.Bool(false)
	w.Bool(true)
	w.VarInt(0)
	h.finishConfig(t, w)
	res := h.result(5 * time.Second)
	require.NoError(t, res.err)
	require.EqualValues(t, 0x15, res.id.SkinParts)
}

// finishConfig plays the client side of configuration after login success.
func (h *harness) finishConfig(t *testing.T, settings ...*javaproto.Writer) {
	h.send(pkt(javaproto.LoginAcknowledged))
	id, r := h.recv()
	require.Equal(t, javaproto.ConfigKnownPacks, id)
	require.EqualValues(t, 1, r.VarInt())
	require.Equal(t, "minecraft", r.String(32))
	require.Equal(t, "core", r.String(32))
	require.Equal(t, "26.1", r.String(32))
	h.send(pkt(javaproto.ConfigSettings)) // a stray settings packet is fine
	for _, w := range settings {
		h.send(w)
	}
	h.send(knownPacksReply("minecraft", "core", "26.1"))
	for _, reg := range java261.Registries() {
		id, r = h.recv()
		require.Equal(t, javaproto.ConfigRegistryData, id)
		require.Equal(t, reg.ID, r.String(256))
		require.EqualValues(t, len(reg.Entries), r.VarInt())
	}
	id, r = h.recv()
	require.Equal(t, javaproto.ConfigFeatureFlags, id)
	require.EqualValues(t, 1, r.VarInt())
	require.Equal(t, "minecraft:vanilla", r.String(64))
	id, r = h.recv()
	require.Equal(t, javaproto.ConfigTags, id)
	require.EqualValues(t, len(java261.Tags()), r.VarInt(), "every tag registry the tables hold")
	require.NoError(t, r.Err())
	id, _ = h.recv()
	require.Equal(t, javaproto.ConfigFinish, id)
	h.send(pkt(javaproto.ConfigFinishFromClient))
}

func TestOfflineLoginReachesPlay(t *testing.T) {
	h := start(t, Options{Offline: true})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Steve")
	id, _ := h.recv()
	require.Equal(t, javaproto.LoginCompress, id)
	h.client.EnableCompression(256)
	id, r := h.recv()
	require.Equal(t, javaproto.LoginSuccess, id)
	u := r.UUID()
	require.Equal(t, uuid.Version(3), u.Version())
	h.finishConfig(t)
	res := h.result(5 * time.Second)
	require.NoError(t, res.err)
	require.Equal(t, u, res.id.UUID)
	require.Equal(t, "Steve", res.id.Name)
}

func TestAuthServerDownGivesMessage(t *testing.T) {
	release := make(chan struct{})
	surl := fakeSession(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	h := start(t, Options{SessionURL: surl})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Notch")
	h.encrypt()
	began := time.Now()
	id, r := h.recv()
	require.Equal(t, javaproto.LoginDisconnect, id)
	require.NotEmpty(t, r.String(32767))
	require.Less(t, time.Since(began), 11*time.Second)
	require.Error(t, h.result(5*time.Second).err)
}

func TestNodeBotReachesPlay(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir, _ := filepath.Abs("../../bots/java")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("run npm install in bots/java first")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	done := make(chan result, 1)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer nc.Close()
		id, err := Accept(javawire.NewConn(nc), Options{Offline: true})
		done <- result{id, err}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	cmd := exec.Command("node", "bot.js", "127.0.0.1", strconv.Itoa(port), "OvermindBot")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("bot output:\n%s", out)
	require.NoError(t, err)
	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Equal(t, "OvermindBot", r.id.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return")
	}
}

func knownPacksReply(packs ...string) *javaproto.Writer {
	w := pkt(javaproto.ConfigKnownPacksReply)
	w.VarInt(int32(len(packs) / 3))
	for _, p := range packs {
		w.String(p)
	}
	return w
}

// toConfiguration runs an offline client up to the known packs question.
func toConfiguration(t *testing.T) *harness {
	h := start(t, Options{Offline: true})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Steve")
	h.recv()
	h.client.EnableCompression(256)
	h.recv()
	h.send(pkt(javaproto.LoginAcknowledged))
	id, _ := h.recv()
	require.Equal(t, javaproto.ConfigKnownPacks, id)
	return h
}

func TestMissingCorePackIsToldSo(t *testing.T) {
	for name, reply := range map[string]*javaproto.Writer{
		"none":          knownPacksReply(),
		"other pack":    knownPacksReply("example", "core", "26.1"),
		"other version": knownPacksReply("minecraft", "core", "1.21"),
	} {
		t.Run(name, func(t *testing.T) {
			h := toConfiguration(t)
			h.send(reply)
			id, _ := h.recv()
			require.Equal(t, javaproto.ConfigDisconnect, id)
			require.Error(t, h.result(5*time.Second).err)
		})
	}
}

func TestZeroOptionsAsksForEncryption(t *testing.T) {
	h := start(t, Options{SessionURL: "http://127.0.0.1:1"})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart("Notch")
	id, _ := h.recv()
	require.Equal(t, javaproto.LoginEncryptionReq, id)
}

func TestLongNameGetsMessage(t *testing.T) {
	h := start(t, Options{Offline: true})
	h.handshake(java261.ProtocolVersion, javaproto.NextLogin)
	h.loginStart(strings.Repeat("a", 17))
	id, r := h.recv()
	require.Equal(t, javaproto.LoginDisconnect, id)
	require.Contains(t, r.String(32767), "name")
	require.Error(t, h.result(5*time.Second).err)
}
