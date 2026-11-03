# Notice

Overmind is built on these open projects. No Mojang code or files and no GeyserMC code or mappings are used.

## Dragonfly

Overmind runs on [Dragonfly](https://github.com/df-mc/dragonfly) (`github.com/df-mc/dragonfly`, pinned at commit `51dd9c1d615f17b70bbeef6ab2cbcee0545e66c0`), a Go Minecraft Bedrock server library. Overmind keeps a modified copy of it at `third_party/dragonfly` (v0.11.6, with its LICENSE); `third_party/dragonfly/OVERMIND-FORK.md` lists the changes.

MIT License. Copyright (c) 2020 Dragonfly Tech.

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## PrismarineJS minecraft-data

Java protocol and block tables come from [PrismarineJS minecraft-data](https://github.com/PrismarineJS/minecraft-data) (MIT). Used from Task 2 onward; credited here so it is not forgotten.

## Other Go libraries

gophertunnel (Bedrock protocol, used by Dragonfly and the test bot), go-toml, testify. Each keeps its own license in the Go module cache.
