# tools

Helper programs for developers.

- `seedcheck`: compares our generator with reference Bedrock worlds, biome and height. `go run ./tools/seedcheck -ref <folder with seedN-maps.npz>`.
- `gentables`: builds `internal/tables/java261` from a minecraft-data checkout kept outside the repo. Run `go run ./tools/gentables -data <checkout>`; it prints counts and records the source commit in each data file.
  With `-azalea <azalea-registry/src of azalea-rs/azalea at its 26.1 commit>` it also writes the menu and block entity type IDs and the tags, which minecraft-data does not have. No Mojang file or data generator output is read.
