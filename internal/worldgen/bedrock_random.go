package worldgen

import "math"

// Bedrock's random numbers, for the Bedrock profile (Task 4e). Written in our own Go from the study of
// BDS 1.26.60.6 (NOTES-bedrock-exact section 1): a textbook 32-bit Mersenne Twister with Bedrock's
// method semantics, which differ from Java's (plain modulo, no rejection, floats rounded from a double).

const (
	mtN = 624
	mtM = 397
)

// bmt is Bedrock's Random: MT19937. Seeding is the standard init_genrand (section 1). Both the seeding
// and the twist are worked out word by word as draws need them, which gives the same output as the
// whole-array version: the twist of word k reads words k+1 and k+397, which are still the seeding's
// (or the last block's) for k < 227 and already twisted after, exactly as the in-place loop reads them.
// Most rules draw a handful of words, so a fresh seed costs about 400 seeding steps, not 1248.
type bmt struct {
	mt     [mtN]uint32
	i      int // The next word to twist and output.
	filled int // Seeding words made so far (mtN once the first block is under way past them).
	seed   uint32
}

func newBMT(seed uint32) *bmt {
	r := &bmt{}
	r.setSeed(seed)
	return r
}

// setSeed restarts the generator.
func (r *bmt) setSeed(seed uint32) {
	r.seed = seed
	r.mt[0] = seed
	r.filled = 1
	r.i = 0
}

// fill makes seeding words up to n.
func (r *bmt) fill(n int) {
	mt := &r.mt
	for k := r.filled; k < n; k++ {
		p := mt[k-1]
		mt[k] = 0x6c078965*(p^p>>30) + uint32(k)
	}
	r.filled = n
}

// u32 is the raw tempered output, which some features draw from directly (section 1).
func (r *bmt) u32() uint32 {
	k := r.i
	if k == mtN {
		k = 0
	}
	if r.filled < mtN {
		if need := min(k+mtM+1, mtN); r.filled < need {
			r.fill(need)
		}
	}
	mt := &r.mt
	k1, km := k+1, k+mtM
	if k1 == mtN {
		k1 = 0
	}
	if km >= mtN {
		km -= mtN
	}
	y := mt[k]&0x80000000 | mt[k1]&0x7fffffff
	v := mt[km] ^ y>>1
	if y&1 != 0 {
		v ^= 0x9908b0df
	}
	mt[k] = v
	r.i = k + 1
	y = v
	y ^= y >> 11
	y ^= y << 7 & 0x9d2c5680
	y ^= y << 15 & 0xefc60000
	y ^= y >> 18
	return y
}

// nextInt is u >> 1 (section 1 table).
func (r *bmt) nextInt() int32 { return int32(r.u32() >> 1) }

// nextIntN is u % n, unsigned, with no draw for n <= 0 (section 1 table).
func (r *bmt) nextIntN(n int32) int32 {
	if n <= 0 {
		return 0
	}
	return int32(r.u32() % uint32(n))
}

// nextBool is bit 27 of a draw (section 1 table).
func (r *bmt) nextBool() bool { return r.u32()>>27&1 != 0 }

// nextFloat is the draw times 2^-32 as a double, rounded to float: it can return 1 (section 1 table).
func (r *bmt) nextFloat() float32 { return float32(float64(r.u32()) * (1.0 / 4294967296.0)) }

// nextDouble is the draw times 2^-32 (section 1 table).
func (r *bmt) nextDouble() float64 { return float64(r.u32()) * (1.0 / 4294967296.0) }

// rawFloat is a raw draw as a float, as the carvers' six-draw steps use (section 5a).
func (r *bmt) rawFloat() float32 { return float32(float64(r.u32()) * (1.0 / 4294967296.0)) }

// bedrockSin is Bedrock's 65536-entry sine table, taken as (float)sin(i*2*PI/65536) (section 4).
var bedrockSin = func() *[65536]float32 {
	var t [65536]float32
	for i := range t {
		t[i] = float32(math.Sin(float64(i) * math.Pi * 2 / 65536))
	}
	return &t
}()

// fnv1Hash is the FNV-1 64-bit hash Bedrock keys rules by (section 2: multiply, then xor).
func fnv1Hash(s string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(s); i++ {
		h *= 0x100000001b3
		h ^= uint64(s[i])
	}
	return h
}

// bedrockLevelSeed is the 32-bit seed decoration draws from: the level seed's low 32 bits
// (sections 2 and P3, checked on 64-bit and negative seeds).
func bedrockLevelSeed(seed int64) uint32 { return uint32(seed) }

// bedrockChunkSeed is a chunk's decoration seed (section 2): R = Random(s); xm = nextInt()|1,
// zm = nextInt()|1; s ^ (cx*xm + cz*zm), in uint32. The cave carvers reseed with the same value.
func bedrockChunkSeed(s uint32, xm, zm uint32, cx, cz int) uint32 {
	return s ^ (uint32(int32(cx))*xm + uint32(int32(cz))*zm)
}

// bedrockSeedMultipliers are the two odd multipliers of a level seed (section 2).
func bedrockSeedMultipliers(s uint32) (xm, zm uint32) {
	r := newBMT(s)
	a, b := uint32(r.nextInt()), uint32(r.nextInt())
	return a/2*2 + 1, b/2*2 + 1
}

// bedrockRuleSeed is a feature rule's seed in a chunk (section 2): boost's hash_combine of the chunk
// seed with the low 32 bits of the FNV-1 hash of the rule identifier.
func bedrockRuleSeed(chunkSeed uint32, ruleHash uint32) uint32 {
	s := chunkSeed
	return s ^ (s>>2 + ruleHash + s<<6 + 0x9e3779b9)
}

// Bedrock floor (NOTES-bedrock-exact R1): a chunk's own MT stream, from two constants found in
// OverworldGenerator::loadChunk, independent of the level seed.
const (
	floorMulX = 0x14609048 // R1: 341872712
	floorMulZ = 0x7ebe2d5  // R1: 132899541
)

// floorRandom gives the chunk's floor stream: MT(cx*0x14609048 + cz*0x7ebe2d5), int32 arithmetic.
func floorRandom(cx, cz int) *bmt {
	return newBMT(uint32(int32(cx)*floorMulX + int32(cz)*floorMulZ))
}

// writeBedrockFloor lays Bedrock's floor over a chunk's noise stage (R1): for x outer, z inner, the
// column gets nextInt(4)+2 bedrock blocks from Y -64 up, whatever is there. Only the columns x0 to
// x1, z0 to z1 are written; every column draws. It returns the stream, which the surface builder
// goes on drawing from.
func writeBedrockFloor(b *baseChunk, x0, x1, z0, z1 int) *bmt {
	r := floorRandom(b.cx, b.cz)
	for x := range 16 {
		for z := range 16 {
			n := int(r.nextIntN(4)) + 2
			if x < x0 || x > x1 || z < z0 || z > z1 {
				continue
			}
			for y := minY; y < minY+n; y++ {
				b.b[cellIndex(x, y, z)] = sBedrock
			}
		}
	}
	return r
}
