# bots/java

A Java 26.1 fake player built on minecraft-protocol. `npm install` here once, then `node bot.js <host> <port> <name>`; it exits 0 when the server finishes configuration and the client enters play. Used by `internal/javalogin` tests. `node_modules` is git-ignored.

`player.js` stays online and is driven over stdin and stdout, one JSON line each way (`internal/javafront/bot_test.go` is the driver). It walks, falls onto the ground it has decoded, digs, sends junk on request, and reports what it saw. Chunks go through prismarine-chunk, so a wrong server encoder shows up as wrong blocks. `BOT_QUIET=1` skips the decoding, for memory runs.
