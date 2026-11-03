# Overmind: notes for Claude agents

- **Go module path: `github.com/creeperdiamonds/overmind`.** Use it for every import. Never `github.com/overmind/overmind` (an old placeholder, removed 2026-11-04).
- GitHub home: `creeperdiamonds/Overmind` (public). Branches there: `restart` (the combined branch, `merge-all` locally) and `restart-world` (`stage-1`, this world branch). Don't push; the owner pushes.
- No Mojang code or files in the repo. Studying Mojang's programs is allowed, but study material stays outside the repo.
- Dragonfly is forked in `third_party/dragonfly`. Keep edits minimal and tag each one "Overmind fork" in `third_party/dragonfly/NOTES.md`.
- The owner uses this PC while agents run: run builds and tests at idle priority with `GOMAXPROCS=4` and `-p 2`.
- Bedrock 1.26.60 clients connect over NetherNet (TCP 19132 plus UDP 19133), not RakNet. A real client is the final test; bots alone have missed real bugs.
