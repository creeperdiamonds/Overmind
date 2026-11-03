# Overmind's Dragonfly fork

Dragonfly v0.11.6 (`github.com/df-mc/dragonfly`, MIT, see LICENSE), copied here from the Go module cache and used through `replace` in Overmind's go.mod, as the design planned. The fork came forward from stage 1 Task 5 to Task 4c fix round 2, the first change Overmind needed inside Dragonfly.

Changes, each marked "Overmind fork" in the code:

- `server/world/tick.go`: a loader's random-tick and entity-tick reach is the world's tick range held at least two chunks under the loader's own chunk radius (`tickCentres`), with its own chunk always ticked. Before, every loader ticked the world's tick range whatever its view distance, so a client asking for a small view distance made ticks read, and generate, chunks it had not loaded. Entities now tick only within that reach, not in every viewed chunk.

To take a new Dragonfly release: copy it over this folder, then reapply the changes above.
