# javalogin

Takes one Java 26.1 (protocol 775) connection through handshake, then status or login, then configuration. `Accept` returns when the client has finished configuration (ready for play), or `ErrStatusServed` for a server list ping.

- Mojang login is on by default: the zero `Options` means RSA key exchange, AES encryption and the session server `hasJoined` check (10 s timeout, answer capped at 64 KiB). Name, UUID and the signed textures come from its answer. `Offline: true` (version 3 UUID of `OfflinePlayer:<name>`) is for bots and tests only.
- The whole exchange has a 30 s deadline. A rejected client gets a disconnect message in the current state, when it can be written.
- The caller must limit connections (per address and in total). Each login holds a goroutine and buffers for up to 30 s, and Mojang login makes an outbound request per attempt.
- Configuration sends the known pack `minecraft:core` 26.1, and the client must answer with that pack or it is told so and disconnected. Then every registry with IDs and no data (a vanilla client with that pack loads the entries itself), feature flags (`minecraft:vanilla`), Update Tags, and finish. The tags packet is encoded once (about 60 KB) and reused.
- The client's settings in configuration give `Identity.SkinParts`, the skin layers it shows (all seven when none came). Settings it cannot read are let through and change nothing, as a stray settings packet always was.
- Tests use a fake session server and never contact Mojang. `TestNodeBotReachesPlay` runs `bots/java/bot.js` and skips if `npm install` has not been run there.
