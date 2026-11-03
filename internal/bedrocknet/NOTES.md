# Bedrock transports: RakNet and NetherNet

Bedrock 26.50 and later join servers over **NetherNet**, not RakNet. A 26.60 client that finds nothing on the server's TCP port does not join at all. A packet capture showed it sending TCP SYNs to 19132 and no UDP. Overmind therefore listens on both:

| What | Where (defaults) | Who uses it |
|---|---|---|
| RakNet | UDP `BedrockAddr` (`:19132`) | older clients, `bots/bedrock`, `tools/memtest` |
| NetherNet signalling (HTTP/1.1) | TCP `BedrockAddr` (`:19132`) | 26.50+ clients: status ping and join |
| NetherNet game traffic (WebRTC) | UDP `NetherNetUDPPorts` (`19133`, one port shared by all players) | 26.50+ clients after signalling |

Open both 19132/udp and 19132/tcp, plus 19133/udp, in a firewall or port forward. Settings (overmind.toml): `BedrockTransports` (default `["raknet", "nethernet"]`), `NetherNetUDPPorts` (`"19133"`, a `"min-max"` range, or `""` for an OS-picked port per player) and `NetherNetKeyFile` (`keys/nethernet-identity.pem`). A settings file written before these keys existed gets the defaults.

## No code of our own on the wire

The transport is [go-nethernet](https://github.com/df-mc/go-nethernet) v1.0.26 (MIT), reached through gophertunnel v1.63.0's `minecraft.NetherNet` network and the Dragonfly fork's `server.NetherNetConfig` (`third_party/dragonfly/server/listener.go`). This package only maps Overmind's settings onto `server.UserConfig.Network`. Neither library needed an upgrade: both were already the newest releases (checked 2026-11-04).

## What the client sends

Sources: Mojang's public [NetherNet HTTP signalling onboarding guide](https://mojang.github.io/bedrock-protocol-docs/guides/nether-net-onboarding-guide/), go-nethernet's source, and the BDS 1.26.60.6 exchange recorded below.

1. **Discovery.** The client sends `GET /v1/join` to `https://host:port`, then to `http://host:port`. Without an explicit port it also tries 443 and 80. We serve plain HTTP. The TLS attempt gets Go's "HTTP request to an HTTPS server" reply, which fails it in about 0.2 s. HTTPS would need a trusted certificate (go-nethernet's `HandlerConfig.ServeTLS`, or a reverse proxy) and is not set up.
2. **Status.** `GET /v1/join` must return 2xx, which tells the client the server speaks NetherNet. The body is JSON `{"name","protocol","version","level","players","maxPlayers","gameType"}`. go-nethernet's handler fills it from the RakNet-style pong that gophertunnel produces, so it follows the player count. BDS 1.26.60.6 returns 200 with an **empty** body here.
3. **Offer.** `POST /v1/join/{clientNetworkId}` with `Content-Type: application/sdp` carries the complete SDP offer with no trickle ICE: host (UDP) candidates, `a=setup:actpass`, one `m=application … UDP/DTLS/SCTP webrtc-datachannel`, `a=max-message-size:262144`. The offer also has a session-level `a=identity`: base64 JSON `{"assertion": "{\"fingerprints\": <detached ES384 JWS over the DTLS fingerprints>, \"token\": <GameServerToken JWT with cpk>}", "idp": {"domain", "protocol": "default"}}`.
4. **Answer.** `200`, `application/sdp`, with the full answer and its own `a=identity`. The token there is a self-signed ES384 JWT with an `x5u` header, a `cpk` claim as a P-384 JWK, and `iat`/`exp` 60 s apart. The fingerprints are signed with that key, and `idp.domain` is `"self"`. A client joining over plain HTTP asks the player once to trust the key (trust on first use). That is why the key is kept in `NetherNetKeyFile`: a new key asks every player again.
5. **WebRTC.** The client runs ICE (server controlled), then DTLS (server `active`), then SCTP. It opens `ReliableDataChannel` (ordered, reliable) and `UnreliableDataChannel` (unordered, no retransmits). Every message starts with a 1-byte header: 0 for the last or only piece, N for N pieces still to come. Only the reliable channel fragments.
6. **Bedrock.** RequestNetworkSettings, NetworkSettings, Login and the rest follow as over RakNet, inside the data channels.

Refusals: an offer without `a=identity` gets error code **37** (`IdentityNotAllowed`) from both BDS and Overmind with login on. With login off, Overmind accepts such offers, which is how the bots join (`ListenConfig.AllowAnonymous = AuthDisabled`).

## What was checked

- **Vanilla BDS 1.26.60.6** (Windows, `server-ip=127.0.0.1`, `server-port=19232`, `online-mode=false`, `transport=nethernet`), probed with `tools/nnprobe`:
  - status: 200, empty body;
  - offer without identity: `200` with body `37`;
  - offer with a self-signed client identity: an answer like the one in item 4, then ICE, DTLS and SCTP over UDP (BDS used ephemeral ports, one per candidate), then RequestNetworkSettings → NetworkSettings → Login (35 KB);
  - the Login was refused with Disconnect reason 46, `NotAuthenticated`. BDS wants an Xbox-signed login even with online-mode off, and we had no Xbox account to give it.
  So everything up to and including the Login packet matches vanilla.
- **Overmind** (`overmind.exe`, default settings, login on), the same probe:
  - status JSON with name, protocol 2225, version 1.26.60 and maxPlayers;
  - anonymous offer refused with code 37;
  - with a client identity: transport, then NetworkSettings and Login, then the login was dropped for having no Xbox chain, as over RakNet.
- **Tests** (`internal/app/nethernet_test.go`): a NetherNet bot joins and walks while a RakNet bot is on the same port; `/v1/join` status; one shared UDP port; the identity key survives a restart. Also `internal/bedrocknet` tests for the defaults and the settings file.

## Not yet seen

- A real 26.60 client with Xbox login against Overmind. Its offer carries a real GameServerToken. go-nethernet checks the token's shape and expiry and the fingerprint signature, then gophertunnel checks the Login. The first owner join is the proof.
- Players behind NAT whose server has no public host candidate. go-nethernet's `HandlerConfig.Credentials` can supply STUN/TURN, and pion can map a public IP (`SettingEngine.SetNAT1To1IPs`), but neither is wired in. On a LAN, or with ports forwarded to a host that has the public IP, host candidates are enough.
- HTTPS signalling, which would remove the trust prompt.

## Tools

`go run ./tools/nnprobe -addr host:port [-selfsigned] [-trace] [-dump] [-v]` pings, signals and logs in step by step. `-dump` prints the SDP both ways.
