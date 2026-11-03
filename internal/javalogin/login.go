// Package javalogin takes a Java 26.1 connection from its handshake through
// status or login to the end of configuration.
package javalogin

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/javawire"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

const (
	loginLimit  = 30 * time.Second
	compressAt  = 256
	versionName = "26.1"
	// maxStray bounds the packets skipped while waiting for a specific one.
	maxStray = 32
)

// ErrStatusServed is returned when the connection was a server list ping.
// It is not a failure.
var ErrStatusServed = errors.New("javalogin: status served")

// Identity is who joined.
type Identity struct {
	Name     string
	UUID     uuid.UUID
	Textures string // base64 "textures" property from Mojang; empty offline
	// TexturesSignature signs Textures; empty if Mojang sent none.
	TexturesSignature string
	// SkinParts is the skin layers the client shows, a bit each, from its
	// settings in configuration: all seven when it sent none.
	SkinParts byte
}

// Options configures Accept.
type Options struct {
	// Offline skips the Mojang account check, so anyone can join under any
	// name. The zero value keeps login on; set this only for bots and tests.
	Offline       bool
	SessionURL    string // defaults to DefaultSessionURL
	MOTD          string
	MaxPlayers    int
	OnlinePlayers func() int
}

var (
	keyOnce sync.Once
	rsaKey  *rsa.PrivateKey
	pubDER  []byte
	keyErr  error
)

// key makes the RSA key on first use and keeps it for the process.
func key() (*rsa.PrivateKey, []byte, error) {
	keyOnce.Do(func() {
		rsaKey, keyErr = rsa.GenerateKey(rand.Reader, 1024)
		if keyErr == nil {
			pubDER, keyErr = x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
		}
	})
	return rsaKey, pubDER, keyErr
}

var (
	tagsOnce sync.Once
	tagsBody []byte
)

// tagsPacket is the Update Tags packet, encoded once and kept (about 60 KB).
func tagsPacket() []byte {
	tagsOnce.Do(func() { tagsBody = javaproto.UpdateTags(java261.Tags()) })
	return tagsBody
}

// reject is an error the client is told about before the connection closes.
type reject struct {
	msg   string
	cause error // for the log; may be nil
}

func (r *reject) Error() string {
	if r.cause != nil {
		return "rejected: " + r.msg + ": " + r.cause.Error()
	}
	return "rejected: " + r.msg
}

func (r *reject) Unwrap() error { return r.cause }

type session struct {
	c     *javawire.Conn
	opts  Options
	state string // "login" or "configuration", picks the disconnect packet
	// skinParts is the last settings' skin layers.
	skinParts byte
}

// Accept runs the connection until the client has finished configuration, or
// until it was a status ping (ErrStatusServed), or fails. The whole thing
// must finish within 30 seconds. On failure the client is told why when
// possible; the caller closes the connection.
func Accept(c *javawire.Conn, opts Options) (id *Identity, err error) {
	s := &session{c: c, opts: opts, state: "login"}
	defer func() {
		if p := recover(); p != nil {
			id, err = nil, fmt.Errorf("javalogin: panic: %v", p)
		}
	}()
	c.SetDeadline(time.Now().Add(loginLimit))
	id, err = s.run()
	var rj *reject
	if errors.As(err, &rj) {
		c.SetDeadline(time.Now().Add(2 * time.Second))
		if s.state == "login" {
			c.WriteFrame(javaproto.DisconnectLogin(rj.msg))
		} else {
			c.WriteFrame(javaproto.DisconnectConfig(rj.msg))
		}
	}
	if err == nil {
		c.SetDeadline(time.Time{})
	}
	return id, err
}

// next reads a frame and splits off its packet ID.
func (s *session) next() (int32, *javaproto.Reader, error) {
	f, err := s.c.ReadFrame()
	if err != nil {
		return 0, nil, err
	}
	r := javaproto.NewReader(f)
	pid := r.VarInt()
	if r.Err() != nil {
		return 0, nil, r.Err()
	}
	return pid, r, nil
}

// expect reads one frame that must have packet ID want.
func (s *session) expect(want int32) (*javaproto.Reader, error) {
	pid, r, err := s.next()
	if err != nil {
		return nil, err
	}
	if pid != want {
		return nil, fmt.Errorf("javalogin: packet %d, wanted %d", pid, want)
	}
	return r, nil
}

func (s *session) run() (*Identity, error) {
	r, err := s.expect(javaproto.HandshakeSetProtocol)
	if err != nil {
		return nil, err
	}
	hs, err := javaproto.ReadHandshake(r)
	if err != nil {
		return nil, err
	}
	switch hs.Next {
	case javaproto.NextStatus:
		return nil, s.status()
	case javaproto.NextLogin:
	case javaproto.NextTransfer:
		return nil, &reject{msg: "Transfers are not accepted"}
	default:
		return nil, fmt.Errorf("javalogin: next state %d", hs.Next)
	}
	if hs.Protocol != java261.ProtocolVersion {
		return nil, &reject{msg: "Please use Minecraft Java " + versionName}
	}
	id, err := s.login()
	if err != nil {
		return nil, err
	}
	s.skinParts = AllSkinParts
	if err := s.configure(); err != nil {
		return nil, err
	}
	id.SkinParts = s.skinParts
	return id, nil
}

// AllSkinParts is every skin layer: cape, jacket, both sleeves, both trouser
// legs and hat (bits 0 to 6, minecraft.wiki "Client Information").
const AllSkinParts = 0x7F

// settings reads a settings packet for the skin layers. One it cannot read
// changes nothing: settings are a client's preference, and a stray or short one
// has always been let through here.
func (s *session) settings(r *javaproto.Reader) {
	if cs, err := javaproto.ReadClientSettings(r); err == nil {
		s.skinParts = cs.SkinParts
	}
}

func (s *session) status() error {
	served := false
	for i := 0; i < 4; i++ {
		pid, r, err := s.next()
		if err != nil {
			if served && errors.Is(err, io.EOF) {
				return ErrStatusServed
			}
			return err
		}
		switch pid {
		case javaproto.StatusRequest:
			if served {
				return errors.New("javalogin: second status request")
			}
			served = true
			if err := s.c.WriteFrame(javaproto.StatusInfo(s.statusJSON())); err != nil {
				return err
			}
		case javaproto.StatusPing:
			v, err := javaproto.ReadLong(r)
			if err != nil {
				return err
			}
			if err := s.c.WriteFrame(javaproto.Pong(v)); err != nil {
				return err
			}
			return ErrStatusServed
		default:
			return fmt.Errorf("javalogin: status packet %d", pid)
		}
	}
	return errors.New("javalogin: too many status packets")
}

func (s *session) statusJSON() string {
	online := 0
	if s.opts.OnlinePlayers != nil {
		online = s.opts.OnlinePlayers()
	}
	motd := s.opts.MOTD
	if motd == "" {
		motd = "Overmind"
	}
	b, _ := json.Marshal(map[string]any{
		"version":     map[string]any{"name": versionName, "protocol": java261.ProtocolVersion},
		"players":     map[string]any{"max": s.opts.MaxPlayers, "online": online},
		"description": map[string]any{"text": motd},
	})
	return string(b)
}

// validName accepts what Mojang account names can be.
func validName(n string) bool {
	if len(n) < 1 || len(n) > 16 {
		return false
	}
	for _, c := range []byte(n) {
		ok := c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !ok {
			return false
		}
	}
	return true
}

// offlineUUID is Java's name-based version 3 UUID of "OfflinePlayer:<name>".
func offlineUUID(name string) uuid.UUID {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return uuid.UUID(sum)
}

func (s *session) login() (*Identity, error) {
	r, err := s.expect(javaproto.LoginStart)
	if err != nil {
		return nil, err
	}
	name, _, err := javaproto.ReadLoginStart(r)
	if err != nil {
		return nil, err
	}
	if !validName(name) {
		return nil, &reject{msg: "Invalid player name"}
	}
	id := &Identity{Name: name, UUID: offlineUUID(name)}
	if !s.opts.Offline {
		if id, err = s.authenticate(name); err != nil {
			return nil, err
		}
	}
	if err := s.c.WriteFrame(javaproto.Compress(compressAt)); err != nil {
		return nil, err
	}
	s.c.EnableCompression(compressAt)
	if err := s.c.WriteFrame(javaproto.Success(id.UUID, id.Name, id.Textures, id.TexturesSignature)); err != nil {
		return nil, err
	}
	if _, err := s.expect(javaproto.LoginAcknowledged); err != nil {
		return nil, err
	}
	s.state = "configuration"
	return id, nil
}

// authenticate does the key exchange and asks the session server.
func (s *session) authenticate(name string) (*Identity, error) {
	priv, pub, err := key()
	if err != nil {
		return nil, err
	}
	token := make([]byte, 4)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	if err := s.c.WriteFrame(javaproto.EncryptionRequest("", pub, token, true)); err != nil {
		return nil, err
	}
	r, err := s.expect(javaproto.LoginEncryptionResp)
	if err != nil {
		return nil, err
	}
	resp, err := javaproto.ReadEncryptionResponse(r)
	if err != nil {
		return nil, err
	}
	secret, err := rsa.DecryptPKCS1v15(nil, priv, resp.Secret)
	if err != nil || len(secret) != 16 {
		return nil, &reject{msg: "Bad encryption response"}
	}
	got, err := rsa.DecryptPKCS1v15(nil, priv, resp.Token)
	if err != nil || !bytes.Equal(got, token) {
		return nil, &reject{msg: "Bad encryption response"}
	}
	// The client sends nothing more until we answer, so nothing encrypted is
	// waiting in the read buffer.
	if err := s.c.EnableEncryption(secret); err != nil {
		return nil, err
	}
	id, err := hasJoined(context.Background(), s.opts.SessionURL, name, ServerHash("", secret, pub))
	switch {
	case errors.Is(err, errNotJoined):
		return nil, &reject{msg: "Failed to verify username. Restart your game and launcher, then try again."}
	case err != nil:
		return nil, &reject{msg: "Login servers are not reachable right now. Please try again later.", cause: err}
	}
	return id, nil
}

// configure sends the known pack, the registries and finish, and waits for
// the client's finish.
func (s *session) configure() error {
	if err := s.c.WriteFrame(javaproto.KnownPacks("minecraft", "core", versionName)); err != nil {
		return err
	}
	// Wait for the client's pack choice, skipping settings and brand.
	for stray := 0; ; stray++ {
		if stray > maxStray {
			return errors.New("javalogin: too many stray configuration packets")
		}
		pid, r, err := s.next()
		if err != nil {
			return err
		}
		if pid == javaproto.ConfigKnownPacksReply {
			packs, err := javaproto.ReadKnownPacksReply(r)
			if err != nil {
				return err
			}
			if !slices.Contains(packs, javaproto.Pack{Namespace: "minecraft", ID: "core", Version: versionName}) {
				return &reject{msg: "This server needs the minecraft:core data pack, version " + versionName}
			}
			break
		}
		if pid == javaproto.ConfigSettings {
			s.settings(r)
			continue
		}
		if pid != javaproto.ConfigCustomPayload {
			return fmt.Errorf("javalogin: configuration packet %d", pid)
		}
	}
	for _, reg := range java261.Registries() {
		if err := s.c.WriteFrame(javaproto.RegistryData(reg.ID, reg.Entries)); err != nil {
			return err
		}
	}
	if err := s.c.WriteFrame(javaproto.FeatureFlags("minecraft:vanilla")); err != nil {
		return err
	}
	if err := s.c.WriteFrame(tagsPacket()); err != nil {
		return err
	}
	if err := s.c.WriteFrame(javaproto.FinishConfiguration()); err != nil {
		return err
	}
	for stray := 0; ; stray++ {
		if stray > maxStray {
			return errors.New("javalogin: too many stray configuration packets")
		}
		pid, r, err := s.next()
		if err != nil {
			return err
		}
		switch pid {
		case javaproto.ConfigFinishFromClient:
			return nil
		case javaproto.ConfigSettings:
			s.settings(r)
		case javaproto.ConfigCustomPayload:
		default:
			return fmt.Errorf("javalogin: configuration packet %d", pid)
		}
	}
}
