package javaproto

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

func id(state, dir, name string) int32 {
	v := java261.PacketID(state, dir, name)
	if v < 0 {
		panic(fmt.Sprintf("javaproto: no packet %s/%s/%s", state, dir, name))
	}
	return v
}

// Packet IDs, read from the generated tables.
var (
	HandshakeSetProtocol = id("handshaking", "toServer", "set_protocol")

	StatusRequest  = id("status", "toServer", "ping_start")
	StatusPing     = id("status", "toServer", "ping")
	StatusResponse = id("status", "toClient", "server_info")
	StatusPong     = id("status", "toClient", "ping")

	LoginStart          = id("login", "toServer", "login_start")
	LoginEncryptionResp = id("login", "toServer", "encryption_begin")
	LoginAcknowledged   = id("login", "toServer", "login_acknowledged")
	LoginDisconnect     = id("login", "toClient", "disconnect")
	LoginEncryptionReq  = id("login", "toClient", "encryption_begin")
	LoginSuccess        = id("login", "toClient", "success")
	LoginCompress       = id("login", "toClient", "compress")

	ConfigCustomPayload    = id("configuration", "toServer", "custom_payload")
	ConfigSettings         = id("configuration", "toServer", "settings")
	ConfigKnownPacksReply  = id("configuration", "toServer", "select_known_packs")
	ConfigFinishFromClient = id("configuration", "toServer", "finish_configuration")
	ConfigKnownPacks       = id("configuration", "toClient", "select_known_packs")
	ConfigRegistryData     = id("configuration", "toClient", "registry_data")
	ConfigFeatureFlags     = id("configuration", "toClient", "feature_flags")
	ConfigTags             = id("configuration", "toClient", "tags")
	ConfigFinish           = id("configuration", "toClient", "finish_configuration")
	ConfigDisconnect       = id("configuration", "toClient", "disconnect")
)

// Next states named in the handshake.
const (
	NextStatus   = 1
	NextLogin    = 2
	NextTransfer = 3
)

// Handshake is the first packet of every connection.
type Handshake struct {
	Protocol int32
	Host     string
	Port     uint16
	Next     int32
}

// ReadHandshake decodes a handshake body whose packet ID was already read.
func ReadHandshake(r *Reader) (Handshake, error) {
	h := Handshake{Protocol: r.VarInt(), Host: r.String(255), Port: r.UShort(), Next: r.VarInt()}
	return h, r.Err()
}

// ReadLoginStart decodes name and UUID. The name is read up to 64 characters so
// the caller can say why a bad one was refused.
func ReadLoginStart(r *Reader) (string, uuid.UUID, error) {
	name := r.String(64)
	u := r.UUID()
	return name, u, r.Err()
}

// EncryptionResponse is the client's reply to an encryption request.
type EncryptionResponse struct{ Secret, Token []byte }

// ReadEncryptionResponse decodes the shared secret and verify token, both
// RSA-encrypted (a 1024-bit key gives 128 bytes each).
func ReadEncryptionResponse(r *Reader) (EncryptionResponse, error) {
	e := EncryptionResponse{Secret: r.Bytes(512), Token: r.Bytes(512)}
	return e, r.Err()
}

// ReadLong decodes a ping payload.
func ReadLong(r *Reader) (int64, error) { v := r.Long(); return v, r.Err() }

// Pack names a data pack in the known packs exchange.
type Pack struct{ Namespace, ID, Version string }

// ReadKnownPacksReply decodes the packs the client chose. More than 64 is
// malformed.
func ReadKnownPacksReply(r *Reader) ([]Pack, error) {
	n := r.VarInt()
	if r.Err() != nil || n < 0 || n > 64 {
		return nil, ErrMalformed
	}
	out := make([]Pack, 0, n)
	for i := int32(0); i < n; i++ {
		out = append(out, Pack{r.String(64), r.String(128), r.String(64)})
	}
	return out, r.Err()
}

func packet(pid int32) *Writer {
	w := &Writer{B: make([]byte, 0, 64)}
	w.VarInt(pid)
	return w
}

// textNBT encodes a plain-string text component as network NBT.
func textNBT(s string) []byte { return appendText(nil, Text{Text: s}) }

// jsonText quotes s as a JSON string component.
func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// StatusInfo builds the server list response; json is the whole JSON object.
func StatusInfo(json string) []byte { w := packet(StatusResponse); w.String(json); return w.B }

// Pong echoes a ping payload.
func Pong(v int64) []byte { w := packet(StatusPong); w.Long(v); return w.B }

// DisconnectLogin ends a login with a message.
func DisconnectLogin(msg string) []byte {
	w := packet(LoginDisconnect)
	w.String(jsonText(msg))
	return w.B
}

// DisconnectConfig ends configuration with a message.
func DisconnectConfig(msg string) []byte {
	w := packet(ConfigDisconnect)
	w.B = append(w.B, textNBT(msg)...)
	return w.B
}

// EncryptionRequest asks the client to encrypt; auth says it must also
// prove its account to the session server.
func EncryptionRequest(serverID string, pub, token []byte, auth bool) []byte {
	w := packet(LoginEncryptionReq)
	w.String(serverID)
	w.Bytes(pub)
	w.Bytes(token)
	w.Bool(auth)
	return w.B
}

// Compress announces the compression threshold.
func Compress(threshold int32) []byte { w := packet(LoginCompress); w.VarInt(threshold); return w.B }

// Success ends the login. A non-empty textures value is sent as the
// "textures" profile property, signed when a signature is given.
func Success(u uuid.UUID, name, textures, signature string) []byte {
	w := packet(LoginSuccess)
	w.UUID(u)
	w.String(name)
	if textures == "" {
		w.VarInt(0)
	} else {
		w.VarInt(1)
		w.String("textures")
		w.String(textures)
		w.Bool(signature != "")
		if signature != "" {
			w.String(signature)
		}
	}
	return w.B
}

// KnownPacks lists one data pack the server and client are expected to share.
func KnownPacks(namespace, packID, version string) []byte {
	w := packet(ConfigKnownPacks)
	w.VarInt(1)
	w.String(namespace)
	w.String(packID)
	w.String(version)
	return w.B
}

// RegistryData names the entries of one registry, none with data.
func RegistryData(registry string, entries []string) []byte {
	w := packet(ConfigRegistryData)
	w.String(registry)
	w.VarInt(int32(len(entries)))
	for _, e := range entries {
		w.String(e)
		w.Bool(false)
	}
	return w.B
}

// FeatureFlags lists the enabled feature flags.
func FeatureFlags(flags ...string) []byte {
	w := packet(ConfigFeatureFlags)
	w.VarInt(int32(len(flags)))
	for _, f := range flags {
		w.String(f)
	}
	return w.B
}

// UpdateTags encodes the tags of every registry given.
func UpdateTags(regs []java261.TagRegistry) []byte {
	w := packet(ConfigTags)
	w.VarInt(int32(len(regs)))
	for _, r := range regs {
		w.String(r.ID)
		w.VarInt(int32(len(r.Tags)))
		for _, t := range r.Tags {
			w.String(t.Name)
			w.VarInt(int32(len(t.IDs)))
			for _, id := range t.IDs {
				w.VarInt(id)
			}
		}
	}
	return w.B
}

// FinishConfiguration ends the configuration phase.
func FinishConfiguration() []byte { return packet(ConfigFinish).B }
