# playerstore

Player data, one JSON file per account (`<uuid>.json`), in Dragonfly's playerdb format through the fork's `playerdb.Snapshot` and `Restore`. One `Store` per edition (`players/bedrock`, `players/java`); it is the `player.Provider` for both.

- **Save** takes the snapshot on the caller's goroutine (the world's): items encoded, nothing else. A writer goroutine marshals and writes: temp file in the same folder, fsync, rename. A crash leaves the old record or the new one. Only the newest pending save of an account is written. `Load` sees pending and in-flight saves, so a quick rejoin never reads an older file.
- **Load** of a record that cannot be read (bad JSON, a bad UUID, an inventory slot out of range, a record for another account, any panic) logs it, renames the file to `<uuid>.json.<UTC time>.corrupt` and returns an error, so the player starts fresh and the bad record is kept. No record gives `ErrNone`, which is not logged.
- **Close** writes everything pending, then stops. Saves after it fail.

The periodic save (`config.SaveSeconds`, default 5) is the hub's (`internal/app`); saves on leave come from Dragonfly (Bedrock) and the Java front end.

On Windows a folder cannot be fsynced, so the rename itself is not forced to disk; a power cut right after it may bring back the previous record.
