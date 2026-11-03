# javawire

Java edition framing only: VarInts, length-prefixed frames, zlib compression
and AES-128 CFB8 encryption. Packet contents live elsewhere.

- Buffers are per connection and reused. A frame buffer grows with the data
  that actually arrives, never straight to the declared length, and anything
  over 64 KiB is dropped before the next frame.
- `MaxFrame` (2 MiB) caps the frame, the declared inflated size and the real
  inflated size.
- Decryption happens below the read buffer. `EnableEncryption` also decrypts
  bytes already buffered, so it must be called from the goroutine that reads
  frames.
- `ReadFrame` returns a slice that is valid until the next call.
- `SetDeadline` and `Close` pass through to the socket, for login time limits.
