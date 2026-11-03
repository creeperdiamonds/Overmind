# testserver

Starts a short-lived server on spare ports so tests can talk to it. `StartIn` keeps the world under a folder you give, so a second call on the same folder is a restart; `TryStartIn` returns the start error instead of failing the test.

Keep a test process to about 20 servers (`serverCap`, 19, what internal/app makes; a start past it fails with the reason, and `Made` gives the count): Dragonfly adds its creative items and recipes to global lists on every server it makes, and after that many a joining bot is dropped (see `docs/stage-2-known-gaps.md`).
