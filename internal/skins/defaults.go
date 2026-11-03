package skins

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/google/uuid"
)

// The default skins both editions share, and how a Java client picks one.
//
// Java 26.1 draws a player with no signed textures in a default skin chosen
// from its UUID: DefaultPlayerSkin.get returns DEFAULT_SKINS[Math.floorMod(
// uuid.hashCode(), DEFAULT_SKINS.length)], DEFAULT_SKINS being these 18, in
// this order: the nine slim (alex, ari, efe, kai, makena, noor, steve, sunny,
// zuri), then the same nine wide (26.1.2 client, decompiled at
// github.com/ma4z-sys/Minecraft-26.1.2, net/minecraft/client/resources/
// DefaultPlayerSkin.java). java.util.UUID.hashCode is (int)(hilo >> 32) ^
// (int)hilo, hilo the most significant 64 bits XOR the least (OpenJDK's
// java/util/UUID.java).
//
// Hash is the SHA-256 of each image's 64x64 RGBA pixels (transparent pixels
// zeroed; skins.Hash), computed outside the repo from the PNGs in the 26.1.2
// client jar (assets/minecraft/textures/entity/player/{slim,wide}/*.png; 26.2
// has the same pixels). No image is shipped. Bedrock's own nine default skins
// (the client's data/skin_packs/vanilla, each on the model skins.json gives
// it: Steve, Ari, Kai and Zuri wide, Alex, Efe, Makena, Noor and Sunny slim)
// have exactly the pixels of Java's image of the same name and model, so a
// Bedrock player in one of them is drawn by Java from its own copy.
var defaultSkins = [18]struct {
	Name string
	Slim bool
	Hash string
}{
	{"alex", true, "a624fd2823a53aeef2f5be22795373e4ebc47767b873dd3f30c7ba708ffd5859"},
	{"ari", true, "d0a2be192a0c09584bca07cd871aabc82741204a146971f2c80d648e9c90295c"},
	{"efe", true, "2ae937d95d733832f54cafdbc678cc2a9164fdc6f459bc4cf9b8f71eaddf9d8e"},
	{"kai", true, "0cc7be4437d95b367858487d23fd52dcd95da4cfe9bbb19ee9f154d8dbbf06b0"},
	{"makena", true, "1a5c2a17187494b9fc3bae6f2fa239cc106d7792d95501830ca89a9d16d148d3"},
	{"noor", true, "7083ad50a9eeb7b3835ac2a37c69af14586aa9c8268e5b68ce7522e4adae597e"},
	{"steve", true, "6fc2ea5aa6c6ccc2de7177f61be91b2d454ca5d884c4bfce7ed23c4431f05d28"},
	{"sunny", true, "09bfdaa149bd8819f91f56e96a6b5819a7e1ebfdb8396d5c90eecea37405bf4f"},
	{"zuri", true, "bdc85d0f191c61273c03f8c79acdec34391608e5638aafe502a5f5b234635a08"},
	{"alex", false, "8c3152db9077a1bc087ff1d344a73e1f7d92c862b124d499aae5af0e44f2058d"},
	{"ari", false, "1b0130c81d9ecb8ac2c407cb563694e8e937a1b75effdd0ff744fa3149e44443"},
	{"efe", false, "4a8e842bf3c434f3d8bd6f3d0c88999691cbf1a1b20ed0b92c4270a6e9adc9bc"},
	{"kai", false, "9c29c65cb53ab2c0ebb1443723f2175c745b8eb21a70887278b388324b1433ed"},
	{"makena", false, "f838de11394b5f1b73e6f04f82c03b476adb750446008e5ef774c98773acc906"},
	{"noor", false, "a4040285f74165973a81228216ed739665ef5ef8dbad1a4b5b42091731f5a6d9"},
	{"steve", false, "c1c3474985ecd2c523ca53e9126a365348b39c7d6a3a03adf896041c8d1c3c69"},
	{"sunny", false, "6824578e43651f2553dfe959aa3ce4e37786f94ccc5f44785ae5bfc4238cd40a"},
	{"zuri", false, "2ce1ae8714ea32dfee169454c2973ad9c3f82b9abeb4ff19ec2e1bf491149efd"},
}

// The fallback default skins: Steve for the wide model, Alex for the slim one.
const (
	SteveWide = 15
	AlexSlim  = 0
)

// Fallback is the default skin for a skin Java cannot be shown: Steve or Alex
// by arm width.
func Fallback(slim bool) int {
	if slim {
		return AlexSlim
	}
	return SteveWide
}

// DefaultIndex is the default skin whose image has this hash on this model,
// and whether there is one.
func DefaultIndex(hash string, slim bool) (int, bool) {
	for i, d := range defaultSkins {
		if d.Hash == hash && d.Slim == slim {
			return i, true
		}
	}
	return 0, false
}

// DefaultName names a default skin, as "wide steve".
func DefaultName(i int) string {
	d := defaultSkins[i]
	if d.Slim {
		return "slim " + d.Name
	}
	return "wide " + d.Name
}

// JavaDefault is the default skin a Java client draws for a UUID.
func JavaDefault(id uuid.UUID) int {
	msb := binary.BigEndian.Uint64(id[:8])
	lsb := binary.BigEndian.Uint64(id[8:])
	hilo := msb ^ lsb
	h := int32(uint32(hilo>>32)) ^ int32(uint32(hilo))
	n := int32(len(defaultSkins))
	return int(((h % n) + n) % n) // Math.floorMod
}

// ViewUUID is the UUID Java viewers know a Bedrock player by, made so that a
// Java client draws default skin want for it. It is the same for the same
// account and skin every session: the first SHA-256 of the account UUID and a
// counter, made an RFC 9562 version 8 (custom) UUID, that hashes to want.
// Mojang's accounts are version 4 and offline Java players version 3, so it is
// no Java account's; skip lists UUIDs it must not be (players online).
func ViewUUID(account uuid.UUID, want int, skip func(uuid.UUID) bool) uuid.UUID {
	var in [16 + 4 + 24]byte
	copy(in[:], "overmind java view uuid:")
	copy(in[24:], account[:])
	for k := uint32(0); ; k++ {
		binary.BigEndian.PutUint32(in[40:], k)
		sum := sha256.Sum256(in[:])
		var id uuid.UUID
		copy(id[:], sum[:16])
		id[6] = id[6]&0x0f | 0x80 // version 8
		id[8] = id[8]&0x3f | 0x80 // RFC 9562 variant
		if JavaDefault(id) == want && (skip == nil || !skip(id)) {
			return id
		}
	}
}
