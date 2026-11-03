# testserver

Starts a short-lived server on spare ports so tests can talk to it. Xbox and Mojang login are off, and it can say where a Java player is. `StartWith` changes the settings first; `StartIn` keeps the world under a folder you give, so a second call on the same folder is a restart; `TryStartIn` returns the start error instead of failing the test.

A test process may make up to `serverCap` (500) servers; a start past it fails with the reason, and `Made` gives the count. The cap was 19 while Dragonfly added its creative items and recipes to global lists on every server it made, which after about 20 servers dropped a joining bot (see `docs/stage-2-known-gaps.md`). The fork now registers them once per process (`vanillaOnce`, `third_party/dragonfly/NOTES.md`), so the cap is only a guard against a runaway test.

`FlatWorld` makes every server the process starts use Dragonfly's flat world (`config.FlatWorld`, never read from the file). internal/javafront's `TestMain` sets it: its tests build at fixed heights on the flat world, as they were written before worldgen.
