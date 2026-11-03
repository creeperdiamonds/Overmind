# give

`/give`, the one command players have (ruling 38 lifts the first release's "no commands" for it alone). Registered once per process with Dragonfly's `cmd` package (`Register`, called by `internal/app`), so both editions run it through `Player.ExecuteCommand`: Bedrock from `CommandRequest`, Java from `chat_command` and `chat_command_signed`.

## Syntax

minecraft.wiki "Commands/give" (oldid=3855978) and "Data component format" (oldid=3852498).

- Java: `/give <targets> <item>[<components>] [<count>]`, for example `minecraft:diamond_sword[enchantments={sharpness:5,unbreaking:3},custom_name='"Blade"',damage=10,unbreakable={}]` or `stone 64`. `minecraft:` is optional.
- Bedrock: `/give <player> <itemName> [amount] [data]`. A data value picks the Bedrock item of that metadata (`potion 1 5`). Bedrock's JSON components (`can_destroy` and the like) are refused with an error.
- Targets are Dragonfly's: a player name of either edition (any case; quoted if it has spaces), `@s`, `@p`, `@a`, `@r`. They are players in the giver's world only: Dragonfly looks targets up in the command's transaction.
- The count is 1 to 100 stacks, as vanilla Java; more is an error. It is given a stack at a time; what does not fit is dropped at the player's feet, with no pickup delay for a Java player and 10 ticks for a Bedrock one, as each edition does. Java also keeps the item for its owner; Dragonfly items have no owner, so anyone may take it.
- The reply is vanilla Java's: "Gave 1 [Diamond Sword] to Steve", or "to 3 players". Java players get it as `commands.give.success.single` or `.multiple` with the item's own translation key (`Reply.JavaText`); Bedrock players the English text. The English item name is the Java ID made readable, so it can differ from the game's ("Diamond Block" for Block of Diamond).

The item is found by its Java ID first (`java261.JavaItemID`, then `BedrockItem`), then by its Bedrock name.

## Components

SNBT is read by `snbt.go` (Java 1.21.5+'s form, which takes the older ones): quoted strings with either quote and escapes, unquoted strings, `true`/`false`, numbers with suffixes, underscores, `0x` and `0b`, lists, typed arrays and compounds.

- `custom_name`, `lore`: text components (a string, a compound with `text`, `color`, the five style flags and `extra`, or a list), or a string of JSON text (`'"Blade"'`, as 1.20.5 to 1.21.4 wrote them). They become a § string, as Dragonfly and Bedrock hold names; a hex colour becomes the nearest of the sixteen, and a translation its fallback or key. Java clients draw the § codes.
- `enchantments`: `{name:level}` (or the older `{levels:{...}}`), any enchantment Dragonfly has, levels 1 to 255, on any item, as vanilla's /give allows.
- `damage`: below the item's durability.
- `unbreakable`: `{}`.
- `dyed_color`: an int, `[r, g, b]` from 0 to 1, or `{rgb:...}`, on leather armour.

Any other component is an error: "not supported" for one Java has, "Unknown component" otherwise. `!name` (removing a default) is an error too.

## Permission

`config.Operators`: names, Java UUIDs, Bedrock XUIDs (`NewOperators`). The hub calls `Join` for each player before it can run a command, with whether it is an operator and its edition, and `Leave` when it goes. The command's `Allow` reads that, so a non-operator is not shown /give (Bedrock's `AvailableCommands`, Java's command tree) and is told the command does not exist (Dragonfly's `commands.generic.unknown` on Bedrock, vanilla's "Unknown or incomplete command" on Java). The registry is process-wide, keyed by entity handle, because Dragonfly's command registry is.

## The line as typed

Dragonfly splits a command's arguments with a CSV reader, which takes a word starting with a quote as quoted and drops the quotes (`lore=["one", "two"]` would lose them). The hub's `HandleCommandExecution` keeps the line as typed (`Remember`), and /give reads its item from it, skipping the target. Without it (another handler), Dragonfly's split is used.
