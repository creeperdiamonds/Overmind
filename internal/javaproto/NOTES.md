# javaproto

Encoders and decoders for the Java packets used in handshake, status, login and configuration (`login.go`) and the play packets the front end uses so far (`play.go`). Packet IDs come from `java261.PacketID` at start-up, never from literals.

- `Reader` keeps its first error and checks every length against the bytes left in the frame, so a hostile length cannot cause an allocation. Strings are limited in characters, byte arrays in bytes.
- `Writer` just appends to a byte slice.
