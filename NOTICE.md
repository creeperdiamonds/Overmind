# Notice

Overmind is built on these open projects. No Mojang code or files and no GeyserMC code or mappings are used.

## Dragonfly

Overmind runs on a fork of [Dragonfly](https://github.com/df-mc/dragonfly) (`github.com/df-mc/dragonfly`, v0.11.6, commit `51dd9c1d615f17b70bbeef6ab2cbcee0545e66c0`), a Go Minecraft Bedrock server library. The fork is in `third_party/dragonfly`, with its own LICENSE; `third_party/dragonfly/NOTES.md` says what we changed.

MIT License. Copyright (c) 2020 Dragonfly Tech.

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## PrismarineJS minecraft-data

Java protocol and block tables come from [PrismarineJS minecraft-data](https://github.com/PrismarineJS/minecraft-data) (MIT), commit `bd8b5c6048e5af9694374b83f25f5b4aade619cc`. `internal/tables/java261/data` and its `testdata` are generated from it by `tools/gentables`.

## azalea

Two tables in `internal/tables/java261/data` come from [azalea](https://github.com/azalea-rs/azalea)'s `azalea-registry/src`, at commit `eeaf1435e81d9cbd8daa0efa22029c1f259a64b5` (its 26.1 commit), read by `tools/gentables` (names and IDs only, no code):

- `static.gz`: the IDs of the `minecraft:menu` and `minecraft:block_entity_type` registries (azalea's `MenuKind` and `BlockEntityKind`).
- `tags.gz`: the block, item, fluid and entity type tags (names and members; block, item and entity type IDs from minecraft-data, fluid IDs in azalea's order).

MIT License. Copyright (c) 2022 mat

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## Test bots

`bots/java` uses [minecraft-protocol](https://github.com/PrismarineJS/node-minecraft-protocol) and [prismarine-chunk](https://github.com/PrismarineJS/prismarine-chunk) (both MIT), installed by npm and not in the repo.

## go-nethernet

Bedrock 26.50+ clients join over NetherNet. [go-nethernet](https://github.com/df-mc/go-nethernet) (`github.com/df-mc/go-nethernet`, v1.0.26) carries it: the HTTP signalling endpoint, the server identity and the WebRTC data channels. Gophertunnel and the Dragonfly fork listen through it; `internal/bedrocknet` turns it on and `tools/nnprobe` dials with it. Its WebRTC stack is [Pion](https://github.com/pion/webrtc) (MIT).

MIT License. Copyright (c) 2019 Dragonfly Tech. The licence text is the same as Dragonfly's, above.

## Other Go libraries

gophertunnel (Bedrock protocol, used by Dragonfly and the test bot), go-toml, testify. Each keeps its own license in the Go module cache.
