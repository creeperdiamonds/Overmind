package javaproto

// Chat packets. Layouts are minecraft-data's for 26.1.
var (
	PlayChatMessage       = id("play", "toServer", "chat_message")
	PlayChatCommand       = id("play", "toServer", "chat_command")
	PlayChatCommandSigned = id("play", "toServer", "chat_command_signed")
)

// Chat limits, in characters, as the game reads them. A chat message is at most
// 256; a command has the protocol's general string limit.
const (
	MaxChatLength    = 256
	MaxCommandLength = 32767
)

// SystemChat shows a message from the server, not from a player: system_chat,
// content then the action-bar flag.
func SystemChat(t Text) []byte {
	w := packet(PlaySystemChat)
	w.B = appendText(w.B, t)
	w.Bool(false) // in the chat, not the action bar
	return w.B
}

// DisguisedChat shows a player's message with no signature: profileless_chat,
// the message, the chat type as a registry reference (its index plus one), the
// sender's name and no target. The client formats it by the chat type, so
// minecraft:chat shows as "<name> message".
func DisguisedChat(msg Text, chatType int32, sender Text) []byte {
	w := packet(PlayDisguisedChat)
	w.B = appendText(w.B, msg)
	w.VarInt(chatType + 1)
	w.B = appendText(w.B, sender)
	w.Bool(false) // no target name
	return w.B
}

// ReadChatMessage decodes chat_message and returns the text. The timestamp,
// salt, signature and acknowledgements are read past: the server signs nothing
// and checks no signature.
func ReadChatMessage(r *Reader) (string, error) {
	msg := r.String(MaxChatLength)
	r.Long() // timestamp
	r.Long() // salt
	if r.Bool() {
		r.take(256) // signature
	}
	r.VarInt() // offset
	r.take(3)  // acknowledged, a fixed 20-bit set
	r.take(1)  // checksum
	return msg, r.Done()
}

// ReadChatCommand decodes chat_command: the command without its slash.
func ReadChatCommand(r *Reader) (string, error) {
	cmd := r.String(MaxCommandLength)
	return cmd, r.Done()
}

// ReadChatCommandSigned decodes chat_command_signed and returns the command; the
// signatures are read past, as for chat. Nothing is allocated by the claimed
// count: a count past the bytes in the frame fails the reader.
func ReadChatCommandSigned(r *Reader) (string, error) {
	cmd := r.String(MaxCommandLength)
	r.Long() // timestamp
	r.Long() // salt
	n := r.VarInt()
	if n < 0 {
		r.fail()
	}
	for i := int32(0); i < n && r.Err() == nil; i++ {
		r.String(MaxCommandLength) // argument name
		r.take(256)                // signature
	}
	r.VarInt() // message count
	r.take(3)  // acknowledged
	r.take(1)  // checksum
	return cmd, r.Done()
}
