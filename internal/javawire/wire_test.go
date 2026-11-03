package javawire

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"
	"testing/iotest"
)

// pipeConn is a net.Conn whose reads come from r and whose writes go to w.
type pipeConn struct {
	net.Conn
	r io.Reader
	w io.Writer
}

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }

func connReading(r io.Reader) *Conn { return NewConn(&pipeConn{r: r, w: io.Discard}) }

func connWriting(w io.Writer) *Conn { return NewConn(&pipeConn{r: bytes.NewReader(nil), w: w}) }

func TestVarIntVectors(t *testing.T) {
	cases := []struct {
		v   int32
		hex string
	}{
		{0, "00"}, {127, "7f"}, {128, "8001"}, {255, "ff01"}, {25565, "ddc701"},
		{2097151, "ffff7f"}, {2147483647, "ffffffff07"}, {-1, "ffffffff0f"},
	}
	for _, c := range cases {
		got := AppendVarInt(nil, c.v)
		if hex.EncodeToString(got) != c.hex {
			t.Errorf("encode %d = %x, want %s", c.v, got, c.hex)
		}
		v, err := ReadVarInt(bytes.NewReader(got))
		if err != nil || v != c.v {
			t.Errorf("decode %s = %d, %v; want %d", c.hex, v, err, c.v)
		}
	}
}

func TestVarIntTooLongFails(t *testing.T) {
	_, err := ReadVarInt(bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}))
	if !errors.Is(err, ErrVarInt) {
		t.Fatalf("err = %v, want ErrVarInt", err)
	}
	// Fifth byte carrying bits beyond 32 is also invalid.
	if _, err := ReadVarInt(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff, 0x1f})); !errors.Is(err, ErrVarInt) {
		t.Fatalf("overflow err = %v, want ErrVarInt", err)
	}
	if _, err := ReadVarInt(bytes.NewReader([]byte{0x80})); err == nil {
		t.Fatal("truncated varint returned no error")
	}
}

func rawFrame(payload []byte) []byte {
	return append(AppendVarInt(nil, int32(len(payload))), payload...)
}

func TestFrameSplitAcrossReads(t *testing.T) {
	payload := bytes.Repeat([]byte("abc"), 100)
	c := connReading(iotest.OneByteReader(bytes.NewReader(rawFrame(payload))))
	got, err := c.ReadFrame()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("got %d bytes, err %v", len(got), err)
	}
}

func TestManyFramesInOneRead(t *testing.T) {
	var in []byte
	want := [][]byte{{1}, {2, 3}, bytes.Repeat([]byte{9}, 300)}
	for _, p := range want {
		in = append(in, rawFrame(p)...)
	}
	c := connReading(bytes.NewReader(in))
	for i, p := range want {
		got, err := c.ReadFrame()
		if err != nil || !bytes.Equal(got, p) {
			t.Fatalf("frame %d: %v, %v", i, got, err)
		}
	}
	if _, err := c.ReadFrame(); err != io.EOF {
		t.Fatalf("after last frame err = %v, want EOF", err)
	}
}

func TestOversizeFrameRejected(t *testing.T) {
	in := AppendVarInt(nil, MaxFrame+1)
	c := connReading(bytes.NewReader(in))
	if _, err := c.ReadFrame(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if cap(c.rbuf) > 0 {
		t.Fatalf("allocated %d bytes for a rejected frame", cap(c.rbuf))
	}
	if err := connWriting(io.Discard).WriteFrame(make([]byte, MaxFrame+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("write err = %v, want ErrTooLarge", err)
	}
}

func TestLyingLengthAllocatesLittle(t *testing.T) {
	// Declares the largest legal frame but sends three bytes.
	in := append(AppendVarInt(nil, MaxFrame), 1, 2, 3)
	c := connReading(bytes.NewReader(in))
	if _, err := c.ReadFrame(); err == nil {
		t.Fatal("short frame accepted")
	}
	if cap(c.rbuf) > 64<<10 {
		t.Fatalf("buffer grew to %d for 3 bytes of data", cap(c.rbuf))
	}
}

func compressedPair(threshold int) (*Conn, *bytes.Buffer) {
	var out bytes.Buffer
	w := connWriting(&out)
	w.EnableCompression(threshold)
	return w, &out
}

func TestCompressionRoundTrip(t *testing.T) {
	w, out := compressedPair(64)
	small, big := []byte("tiny"), bytes.Repeat([]byte("compress me "), 500)
	for _, p := range [][]byte{small, big, small} {
		if err := w.WriteFrame(p); err != nil {
			t.Fatal(err)
		}
	}
	r := connReading(iotest.OneByteReader(out))
	r.EnableCompression(64)
	for i, p := range [][]byte{small, big, small} {
		got, err := r.ReadFrame()
		if err != nil || !bytes.Equal(got, p) {
			t.Fatalf("frame %d: %d bytes, %v", i, len(got), err)
		}
	}
}

func TestCompressionThreshold(t *testing.T) {
	w, out := compressedPair(64)
	payload := bytes.Repeat([]byte{7}, 63)
	if err := w.WriteFrame(payload); err != nil {
		t.Fatal(err)
	}
	// frame length, data length 0, raw payload.
	want := append(AppendVarInt(nil, int32(len(payload)+1)), 0)
	want = append(want, payload...)
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("below threshold wrote %x", out.Bytes())
	}
	out.Reset()
	payload = bytes.Repeat([]byte{7}, 64)
	if err := w.WriteFrame(payload); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	_, n := mustVarInt(t, b)
	dl, _ := mustVarInt(t, b[n:])
	if dl != 64 {
		t.Fatalf("data length = %d, want 64", dl)
	}
}

func mustVarInt(t *testing.T, b []byte) (int32, int) {
	t.Helper()
	r := bytes.NewReader(b)
	v, err := ReadVarInt(r)
	if err != nil {
		t.Fatal(err)
	}
	return v, len(b) - r.Len()
}

func TestZipBombRejected(t *testing.T) {
	// A frame that declares MaxFrame but inflates to MaxFrame+1000.
	w, out := compressedPair(1)
	if err := w.writeDeflated(bytes.Repeat([]byte{0}, MaxFrame+1000), MaxFrame); err != nil {
		t.Fatal(err)
	}
	r := connReading(bytes.NewReader(out.Bytes()))
	r.EnableCompression(1)
	if _, err := r.ReadFrame(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Declared size above the limit is refused before inflating.
	body := AppendVarInt(nil, MaxFrame+1)
	body = append(body, 0x78, 0x9c)
	in := append(AppendVarInt(nil, int32(len(body))), body...)
	r = connReading(bytes.NewReader(in))
	r.EnableCompression(1)
	if _, err := r.ReadFrame(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("declared err = %v, want ErrTooLarge", err)
	}
}

// refCFB8 is a direct transcription of the CFB8 definition.
func refCFB8(key, iv, data []byte, decrypt bool) []byte {
	blk, _ := aes.NewCipher(key)
	reg := append([]byte(nil), iv...)
	out := make([]byte, len(data))
	for i, b := range data {
		ks := make([]byte, 16)
		blk.Encrypt(ks, reg)
		out[i] = b ^ ks[0]
		ct := out[i]
		if decrypt {
			ct = b
		}
		reg = append(reg[1:], ct)
	}
	return out
}

func TestCFB8Vector(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := make([]byte, 1000)
	for i := range plain {
		plain[i] = byte(i * 7)
	}
	want := refCFB8(key, key, plain, false)

	blk, _ := aes.NewCipher(key)
	enc, dec := newCFB8(blk, key, false), newCFB8(blk, key, true)
	ct := append([]byte(nil), plain...)
	for off, step := 0, 1; off < len(ct); step += 3 {
		end := min(off+step, len(ct))
		enc.XORKeyStream(ct[off:end], ct[off:end])
		off = end
	}
	if !bytes.Equal(ct, want) {
		t.Fatal("ciphertext differs from reference CFB8")
	}
	for off, step := 0, 5; off < len(ct); step += 2 {
		end := min(off+step, len(ct))
		dec.XORKeyStream(ct[off:end], ct[off:end])
		off = end
	}
	if !bytes.Equal(ct, plain) {
		t.Fatal("decrypt did not return the input")
	}
}

func TestEncryptedConnRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef")
	var out bytes.Buffer
	w := connWriting(&out)
	w.EnableCompression(32)
	if err := w.EnableEncryption(secret); err != nil {
		t.Fatal(err)
	}
	p := bytes.Repeat([]byte("hello "), 50)
	if err := w.WriteFrame(p); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte("hello")) {
		t.Fatal("plaintext on the wire")
	}
	r := connReading(iotest.OneByteReader(&out))
	r.EnableCompression(32)
	if err := r.EnableEncryption(secret); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadFrame()
	if err != nil || !bytes.Equal(got, p) {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}
	if err := r.EnableEncryption([]byte("short")); err == nil {
		t.Fatal("bad secret accepted")
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add(byte(0), rawFrame([]byte("hi")))
	f.Add(byte(1), []byte{0x05, 0x03, 'a', 'b', 'c', 'd'})
	f.Add(byte(2), append(AppendVarInt(nil, MaxFrame), 0xff, 0xff))
	f.Fuzz(func(t *testing.T, mode byte, data []byte) {
		c := connReading(bytes.NewReader(data))
		if mode&1 != 0 {
			c.EnableCompression(int(mode >> 1))
		}
		if mode&0x80 != 0 {
			_ = c.EnableEncryption([]byte("0123456789abcdef"))
		}
		for i := 0; i < 8; i++ {
			p, err := c.ReadFrame()
			if err != nil {
				return
			}
			if len(p) > MaxFrame {
				t.Fatalf("frame of %d bytes", len(p))
			}
		}
	})
}

func TestCFB8NISTVector(t *testing.T) {
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	iv, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	pt, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172aae2d")
	want, _ := hex.DecodeString("3b79424c9c0dd436bace9e0ed4586a4f32b9")
	blk, _ := aes.NewCipher(key)
	got := make([]byte, len(pt))
	newCFB8(blk, iv, false).XORKeyStream(got, pt)
	if !bytes.Equal(got, want) {
		t.Fatalf("encrypt = %x, want %x", got, want)
	}
	back := make([]byte, len(got))
	newCFB8(blk, iv, true).XORKeyStream(back, got)
	if !bytes.Equal(back, pt) {
		t.Fatalf("decrypt = %x", back)
	}
}

func TestEncryptionCoversAlreadyBufferedBytes(t *testing.T) {
	secret := []byte("0123456789abcdef")
	var encOut bytes.Buffer
	w := connWriting(&encOut)
	if err := w.EnableEncryption(secret); err != nil {
		t.Fatal(err)
	}
	second := []byte("encrypted second frame")
	if err := w.WriteFrame(second); err != nil {
		t.Fatal(err)
	}
	first := []byte("plain first frame")
	in := append(rawFrame(first), encOut.Bytes()...) // one read delivers both
	r := connReading(bytes.NewReader(in))
	got, err := r.ReadFrame()
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("first: %q, %v", got, err)
	}
	if err := r.EnableEncryption(secret); err != nil {
		t.Fatal(err)
	}
	got, err = r.ReadFrame()
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("second: %q, %v", got, err)
	}
}

func TestEmptyCompressedFrame(t *testing.T) {
	r := connReading(bytes.NewReader([]byte{0}))
	r.EnableCompression(10)
	if _, err := r.ReadFrame(); err != io.ErrUnexpectedEOF {
		t.Fatalf("err = %v, want ErrUnexpectedEOF", err)
	}
}

func TestDataLengthBelowThresholdRejected(t *testing.T) {
	w, out := compressedPair(1)
	if err := w.writeDeflated([]byte("tiny"), 4); err != nil {
		t.Fatal(err)
	}
	r := connReading(bytes.NewReader(out.Bytes()))
	r.EnableCompression(100)
	if _, err := r.ReadFrame(); err == nil {
		t.Fatal("below-threshold compressed frame accepted")
	}
}

func TestBadZlibTrailerRejected(t *testing.T) {
	w, out := compressedPair(1)
	if err := w.WriteFrame(bytes.Repeat([]byte("x"), 50)); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	b[len(b)-1] ^= 0xff // corrupt the Adler-32 checksum
	r := connReading(bytes.NewReader(b))
	r.EnableCompression(1)
	if _, err := r.ReadFrame(); err == nil {
		t.Fatal("corrupt trailer accepted")
	}
}

func TestWriteRefusesFrameBodyOverMaxFrame(t *testing.T) {
	w, _ := compressedPair(MaxFrame + 10)
	// Stored raw: body is payload plus the zero data-length byte.
	if err := w.WriteFrame(make([]byte, MaxFrame)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Incompressible data deflates to more than it came in as.
	w, _ = compressedPair(1)
	noise := make([]byte, MaxFrame)
	x := uint32(1)
	for i := range noise {
		x = x*1664525 + 1013904223
		noise[i] = byte(x >> 24)
	}
	if err := w.WriteFrame(noise); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("deflated err = %v, want ErrTooLarge", err)
	}
}

func TestLyingInflateStreamAllocatesLittle(t *testing.T) {
	// Declares MaxFrame but the zlib stream holds 10 bytes.
	w, out := compressedPair(1)
	if err := w.writeDeflated([]byte("0123456789"), MaxFrame); err != nil {
		t.Fatal(err)
	}
	r := connReading(bytes.NewReader(out.Bytes()))
	r.EnableCompression(1)
	if _, err := r.ReadFrame(); err == nil {
		t.Fatal("short stream accepted")
	}
	if cap(r.ubuf) > 64<<10 {
		t.Fatalf("ubuf grew to %d for 10 bytes of output", cap(r.ubuf))
	}
}
