package javawire

import (
	"compress/zlib"
	"fmt"
	"io"
)

// EnableCompression turns on the compressed frame layout. Payloads of at
// least threshold bytes are deflated when written; a negative threshold
// turns compression off.
func (c *Conn) EnableCompression(threshold int) {
	c.threshold.Store(int64(threshold))
}

// inflate expands the zlib stream in src into c.ubuf, which must come out
// at exactly size bytes. The buffer grows with the real output, so a stream
// that declares much and delivers little costs little.
func (c *Conn) inflate(src []byte, size int) ([]byte, error) {
	c.zsrc.Reset(src)
	if c.zr == nil {
		zr, err := zlib.NewReader(&c.zsrc)
		if err != nil {
			return nil, fmt.Errorf("javawire: inflate: %w", err)
		}
		c.zr = zr
	} else if err := c.zr.(zlib.Resetter).Reset(&c.zsrc, nil); err != nil {
		return nil, fmt.Errorf("javawire: inflate: %w", err)
	}
	out, err := readN(c.zr, c.ubuf, size)
	c.ubuf = out
	if err != nil {
		return nil, fmt.Errorf("javawire: inflate: %w", err)
	}
	var probe [1]byte
	n, err := c.zr.Read(probe[:])
	if n > 0 {
		return nil, ErrTooLarge
	}
	if err != io.EOF {
		return nil, fmt.Errorf("javawire: inflate: %w", err)
	}
	return out, nil
}

// writeDeflated appends to c.wbuf the frame body for payload with the given
// declared data length (normally len(payload)).
func (c *Conn) writeDeflated(payload []byte, declared int) error {
	c.zout.b = c.zout.b[:0]
	if c.zw == nil {
		c.zw, _ = zlib.NewWriterLevel(&c.zout, zlib.BestSpeed)
	} else {
		c.zw.Reset(&c.zout)
	}
	if _, err := c.zw.Write(payload); err != nil {
		return err
	}
	if err := c.zw.Close(); err != nil {
		return err
	}
	dl := AppendVarInt(c.hdr[:0], int32(declared))
	if len(dl)+len(c.zout.b) > MaxFrame {
		return ErrTooLarge
	}
	c.wbuf = AppendVarInt(c.wbuf[:0], int32(len(dl)+len(c.zout.b)))
	c.wbuf = append(c.wbuf, dl...)
	c.wbuf = append(c.wbuf, c.zout.b...)
	return c.send()
}

type sliceWriter struct{ b []byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
