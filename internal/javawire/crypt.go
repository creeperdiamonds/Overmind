package javawire

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

// cfb8 is AES in 8-bit cipher feedback mode, which the standard library
// does not provide. It implements cipher.Stream and may work in place.
type cfb8 struct {
	b       cipher.Block
	reg     [aes.BlockSize]byte
	ks      [aes.BlockSize]byte
	decrypt bool
}

func newCFB8(b cipher.Block, iv []byte, decrypt bool) *cfb8 {
	x := &cfb8{b: b, decrypt: decrypt}
	copy(x.reg[:], iv)
	return x
}

func (x *cfb8) XORKeyStream(dst, src []byte) {
	for i, in := range src {
		x.b.Encrypt(x.ks[:], x.reg[:])
		out := in ^ x.ks[0]
		fed := out
		if x.decrypt {
			fed = in
		}
		copy(x.reg[:], x.reg[1:])
		x.reg[aes.BlockSize-1] = fed
		dst[i] = out
	}
}

// EnableEncryption switches both directions to AES-128 CFB8, using secret as
// key and IV. Call it from the goroutine that calls ReadFrame, after the last
// plaintext frame was read and written. Bytes of the next frame that were
// already buffered are decrypted here.
func (c *Conn) EnableEncryption(secret []byte) error {
	if len(secret) != aes.BlockSize {
		return errors.New("javawire: shared secret must be 16 bytes")
	}
	blk, err := aes.NewCipher(secret)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	c.enc = newCFB8(blk, secret, false)
	c.wmu.Unlock()
	dec := newCFB8(blk, secret, true)
	// Bytes already buffered arrived before the decryptor existed.
	if n := c.br.Buffered(); n > 0 {
		buf, _ := c.br.Peek(n)
		dec.XORKeyStream(buf, buf)
	}
	c.dec.Store(dec)
	return nil
}
