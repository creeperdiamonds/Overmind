# memtest

Measures the server's memory use while fake players walk around.

`-bots` and `-java` set how many of each; `-walk=false` makes them stand still. `go run ./tools/memtest -pid <pid> -java 20 -javaaddr 127.0.0.1:25565 -script bots/java/player.js`. The server must have `JavaOffline = true`.
