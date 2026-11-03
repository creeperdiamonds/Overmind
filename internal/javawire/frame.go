package javawire

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// MaxFrame bounds a frame body, and the declared size of an inflated one.
const MaxFrame = 2 << 20

// retain is the largest buffer a connection keeps between frames. A bigger
// one served a single large packet and is dropped before the next.
const retain = 64 << 10

// firstChunk is how far a buffer grows before any data has arrived to
// justify more.
const firstChunk = 4 << 10

// ErrTooLarge reports a frame, declared size or inflated size over MaxFrame.
var ErrTooLarge = errors.New("javawire: frame too large")

// Conn frames a net.Conn. One goroutine may call ReadFrame while another
// calls WriteFrame; neither may be called concurrently with itself, except
// WriteFrame, which is serialised.
type Conn struct {
	nc net.Conn
	br *bufio.Reader

	threshold atomic.Int64 // compression threshold; negative means off
	dec       atomic.Pointer[cfb8]

	// Read side. Buffers are reused from frame to frame.
	rbuf, ubuf []byte
	zr         io.ReadCloser
	zsrc       bytes.Reader

	// Write side, guarded by wmu.
	wmu  sync.Mutex
	enc  *cfb8
	wbuf []byte
	zout sliceWriter
	zw   *zlib.Writer
	hdr  [5]byte
}

// decReader decrypts bytes as they come off the socket, before buffering.
type decReader Conn

func (d *decReader) Read(p []byte) (int, error) {
	n, err := d.nc.Read(p)
	if x := d.dec.Load(); x != nil {
		x.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

// NewConn wraps nc. Compression and encryption start off.
func NewConn(nc net.Conn) *Conn {
	c := &Conn{nc: nc}
	c.threshold.Store(-1)
	c.br = bufio.NewReaderSize((*decReader)(c), 4096)
	return c
}

// ReadFrame returns the next frame body, inflated if compression is on. The
// slice is only valid until the next call.
func (c *Conn) ReadFrame() ([]byte, error) {
	if cap(c.rbuf) > retain {
		c.rbuf = nil
	}
	if cap(c.ubuf) > retain {
		c.ubuf = nil
	}
	n, err := ReadVarInt(c.br)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > MaxFrame {
		return nil, ErrTooLarge
	}
	c.rbuf, err = readN(c.br, c.rbuf, int(n))
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	if c.threshold.Load() < 0 {
		return c.rbuf, nil
	}
	r := bytes.NewReader(c.rbuf)
	dl, err := ReadVarInt(r)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	if dl < 0 || dl > MaxFrame {
		return nil, ErrTooLarge
	}
	if dl != 0 && int64(dl) < c.threshold.Load() {
		return nil, errors.New("javawire: compressed frame below the threshold")
	}
	if dl == 0 {
		return c.rbuf[len(c.rbuf)-r.Len():], nil
	}
	return c.inflate(c.rbuf[len(c.rbuf)-r.Len():], int(dl))
}

// readN reads exactly n bytes into buf, reusing its storage. It grows the
// buffer in step with data actually received, never straight to n, so a peer
// that claims a large frame and stalls pins little memory.
func readN(r io.Reader, buf []byte, n int) ([]byte, error) {
	buf = buf[:0]
	for len(buf) < n {
		if len(buf) == cap(buf) {
			nb := make([]byte, len(buf), min(n, max(2*cap(buf), firstChunk)))
			copy(nb, buf)
			buf = nb
		}
		end := min(cap(buf), n)
		m, err := io.ReadFull(r, buf[len(buf):end])
		buf = buf[:len(buf)+m]
		if err != nil {
			return buf, err
		}
	}
	return buf, nil
}

// WriteFrame sends payload as one frame.
func (c *Conn) WriteFrame(payload []byte) error {
	if len(payload) > MaxFrame {
		return ErrTooLarge
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if cap(c.wbuf) > retain {
		c.wbuf = nil
	}
	if cap(c.zout.b) > retain {
		c.zout.b = nil
	}
	t := c.threshold.Load()
	switch {
	case t < 0:
		c.wbuf = AppendVarInt(c.wbuf[:0], int32(len(payload)))
		c.wbuf = append(c.wbuf, payload...)
	case int64(len(payload)) < t:
		if len(payload)+1 > MaxFrame {
			return ErrTooLarge
		}
		c.wbuf = AppendVarInt(c.wbuf[:0], int32(len(payload)+1))
		c.wbuf = append(c.wbuf, 0)
		c.wbuf = append(c.wbuf, payload...)
	default:
		return c.writeDeflated(payload, len(payload))
	}
	return c.send()
}

// send encrypts and writes c.wbuf. The caller holds wmu.
func (c *Conn) send() error {
	if c.enc != nil {
		c.enc.XORKeyStream(c.wbuf, c.wbuf)
	}
	if _, err := c.nc.Write(c.wbuf); err != nil {
		return fmt.Errorf("javawire: write: %w", err)
	}
	return nil
}

// SetDeadline limits all reads and writes on the underlying connection; the
// zero time clears it.
func (c *Conn) SetDeadline(t time.Time) error { return c.nc.SetDeadline(t) }

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.nc.Close() }
