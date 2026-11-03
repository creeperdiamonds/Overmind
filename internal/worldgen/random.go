package worldgen

import (
	"crypto/md5"
	"encoding/binary"
	"math/bits"
)

// The game's world seeding, reproduced so that a seed gives Mojang's terrain and biomes. Per the
// owner's decision these layers follow Mojang's algorithms, written here in our own Go from the
// study's notes (NOTES-exact-seed.md sections 1 and 2). No Mojang code is in this repo.

const (
	seedGolden uint64 = 0x9E3779B97F4A7C15
	seedSilver uint64 = 0x6A09E667F3BCC909
)

// xoro is an xoroshiro128++ generator.
type xoro struct{ lo, hi uint64 }

func newXoro(lo, hi uint64) xoro {
	if lo == 0 && hi == 0 {
		return xoro{seedGolden, seedSilver}
	}
	return xoro{lo, hi}
}

// xoroFromSeed starts a generator from a 64-bit integer. Only the world seed root uses this.
func xoroFromSeed(seed int64) xoro {
	mix := func(z uint64) uint64 {
		z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
		z = (z ^ z>>27) * 0x94D049BB133111EB
		return z ^ z>>31
	}
	lo := uint64(seed) ^ seedSilver
	return newXoro(mix(lo), mix(lo+seedGolden))
}

func (r *xoro) next() uint64 {
	lo, hi := r.lo, r.hi
	out := bits.RotateLeft64(lo+hi, 17) + lo
	h := hi ^ lo
	r.lo = bits.RotateLeft64(lo, 49) ^ h ^ h<<21
	r.hi = bits.RotateLeft64(h, 28)
	return out
}

func (r *xoro) nextDouble() float64 {
	return float64(float64(r.next()>>11) * (1.0 / (1 << 53)))
}

// nextFloat is the generator's direct float draw: the top 24 bits.
func (r *xoro) nextFloat() float32 {
	return float32(r.next()>>40) * (1.0 / (1 << 24))
}

// nextInt is the unbiased multiply-shift draw below bound.
func (r *xoro) nextInt(bound uint32) int {
	m := uint64(uint32(r.next())) * uint64(bound)
	if frac := uint32(m); frac < bound {
		threshold := -bound % bound
		for frac < threshold {
			m = uint64(uint32(r.next())) * uint64(bound)
			frac = uint32(m)
		}
	}
	return int(m >> 32)
}

// forkFactory takes a positional factory from a generator. The first draw is its low half.
func (r *xoro) forkFactory() seedFactory {
	lo := r.next()
	return seedFactory{lo, r.next()}
}

// seedFactory makes generators from a name or a position. It never advances.
type seedFactory struct{ lo, hi uint64 }

func (f seedFactory) named(name string) xoro {
	d := md5.Sum([]byte(name))
	return newXoro(binary.BigEndian.Uint64(d[:8])^f.lo, binary.BigEndian.Uint64(d[8:])^f.hi)
}

func (f seedFactory) at(x, y, z int32) xoro {
	return newXoro(uint64(positionSeed(x, y, z))^f.lo, f.hi)
}

// positionSeed mixes a position. The product with x overflows in 32 bits and is then sign extended;
// the one with z is 64-bit.
func positionSeed(x, y, z int32) int64 {
	s := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	s = s*s*42317861 + s*11
	return s >> 16
}

// lcg is the 48-bit linear generator the study calls LCG: carvers, carver tunnels and the fixed
// simplex noises use it (NOTES-exact-features.md section 2).
type lcg struct{ state int64 }

const (
	lcgMul  = 0x5DEECE66D
	lcgMask = (1 << 48) - 1
)

func newLCG(seed int64) lcg { return lcg{(seed ^ lcgMul) & lcgMask} }

func (r *lcg) setSeed(seed int64) { r.state = (seed ^ lcgMul) & lcgMask }

func (r *lcg) bits(n uint) int32 {
	r.state = (r.state*lcgMul + 11) & lcgMask
	return int32(r.state >> (48 - n))
}

func (r *lcg) nextLong() int64 {
	hi := int64(r.bits(32))
	return hi<<32 + int64(r.bits(32))
}

func (r *lcg) nextFloat() float32 { return float32(r.bits(24)) * (1.0 / (1 << 24)) }

func (r *lcg) nextDouble() float64 {
	hi := int64(r.bits(26))
	return float64(hi<<27+int64(r.bits(27))) * (1.0 / (1 << 53))
}

func (r *lcg) nextInt(n int32) int32 {
	if n&(n-1) == 0 {
		return int32((int64(n) * int64(r.bits(31))) >> 31)
	}
	for {
		s := r.bits(31)
		m := s % n
		if s-m+(n-1) >= 0 {
			return m
		}
	}
}
