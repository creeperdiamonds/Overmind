# skins

Edition-neutral skin logic: what Java can draw of a Bedrock skin (`Classify`), the default skins both editions share by pixel hash and the UUID that makes a Java client draw one (`defaults.go`), the odd-shape check (`shape.go`), our stand-in skin (`standin.go`), the uploader that has Mojang sign a skin through a configured Java account (`upload.go`) and that account's sign-in (`account.go`). How the Java front end uses it, the sources, the account setup and its risk are in `internal/javafront/NOTES.md`.

No Mojang image is in the repo: the default skins are known by SHA-256 hashes only. `skintest` makes test skins from gophertunnel's geometry in the module cache and fakes Mojang's services; nothing here calls Mojang, Microsoft or Xbox Live in a test.
