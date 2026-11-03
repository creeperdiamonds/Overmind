package java261

import (
	"strconv"
	"strings"
	"sync"
)

// --- Java item names ---

var (
	javaItemsOnce sync.Once
	javaItemIDs   map[string]int32
)

// JavaItemID is the Java item ID of a Java item name, with or without the
// minecraft: prefix, or -1 when unknown (data/javaitems.gz, minecraft-data's
// pc items.json).
func JavaItemID(name string) int32 {
	javaItemsOnce.Do(func() {
		ls := lines("javaitems")
		javaItemIDs = make(map[string]int32, len(ls))
		for _, l := range ls {
			f := fields(l)
			javaItemIDs[f[0]] = int32(atoi(f[1]))
		}
	})
	if id, ok := javaItemIDs[strings.TrimPrefix(name, prefix)]; ok {
		return id
	}
	return -1
}

// --- static registries ---

var (
	staticOnce sync.Once
	staticIDs  map[string]int32
)

func staticID(registry, name string) int32 {
	staticOnce.Do(func() {
		staticIDs = map[string]int32{}
		for _, l := range lines("static") {
			f := fields(l)
			staticIDs[f[0]+"/"+f[1]] = int32(atoi(f[2]))
		}
	})
	if !strings.HasPrefix(name, prefix) {
		name = prefix + name
	}
	if id, ok := staticIDs[registry+"/"+name]; ok {
		return id
	}
	return -1
}

// MenuType is the ID of a window type in the minecraft:menu registry (data/
// static.gz, from azalea-rs/azalea's registry at its 26.1 commit, which agrees
// with minecraft.wiki's Inventory page oldid=3201379)
// ("generic_9x3", "furnace"), as open_window carries it, or -1.
func MenuType(name string) int32 { return staticID("minecraft:menu", name) }

// BlockEntityType is the ID of a block entity type ("chest", "sign"), as chunk
// data and block_entity_data carry it, or -1.
func BlockEntityType(name string) int32 { return staticID("minecraft:block_entity_type", name) }

// --- crafting recipes ---

// Recipe is one Java crafting recipe (data/recipes.gz: minecraft-data's 26.1
// recipes with gentables' cited corrections). A shaped recipe has W by H cells, row by row, each the Java item
// IDs it accepts, nil for a cell that must be empty. A shapeless one has its
// ingredients in Cells and W, H zero.
type Recipe struct {
	Shaped bool
	W, H   int
	Cells  [][]int32
	Result int32
	Count  int
}

var (
	recipesOnce sync.Once
	recipes     []Recipe
)

// CraftingRecipes is every Java crafting recipe the tables hold. Special
// recipes, which the game works out in code (fireworks, banner copying,
// dyeing, transmuting, repairing and the like), are not among them.
func CraftingRecipes() []Recipe {
	recipesOnce.Do(func() {
		for _, l := range lines("recipes") {
			f := fields(l)
			cell := func(s string) []int32 {
				if s == "-" {
					return nil
				}
				var ids []int32
				for _, n := range strings.Split(s, "|") {
					if id := JavaItemID(n); id >= 0 {
						ids = append(ids, id)
					}
				}
				if ids == nil {
					ids = []int32{} // nothing Java has: matches no item
				}
				return ids
			}
			var r Recipe
			var rest []string
			switch f[0] {
			case "shaped":
				r = Recipe{Shaped: true, W: atoi(f[1]), H: atoi(f[2]), Result: JavaItemID(f[3]), Count: atoi(f[4])}
				rest = f[5:]
			case "shapeless":
				r = Recipe{Result: JavaItemID(f[1]), Count: atoi(f[2])}
				rest = f[3:]
			default:
				panic("java261: unknown recipe kind " + f[0])
			}
			for _, s := range rest {
				r.Cells = append(r.Cells, cell(s))
			}
			if r.Result >= 0 {
				recipes = append(recipes, r)
			}
		}
	})
	return recipes
}

// Craft finds the Java recipe a crafting grid makes, by Java's rules
// (minecraft.wiki, "Crafting", oldid=3870665): a shaped recipe matches
// anywhere in the grid and also mirrored left to right, and a shapeless one
// matches its ingredients in any cells. grid is size by size Java item IDs,
// row by row, -1 for an empty cell. ok is false when nothing matches.
func Craft(grid []int32, size int) (r Recipe, ok bool) {
	minX, minY, maxX, maxY := size, size, -1, -1
	filled := 0
	for i, id := range grid {
		if id < 0 {
			continue
		}
		filled++
		x, y := i%size, i/size
		minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
	}
	if filled == 0 {
		return Recipe{}, false
	}
	w, h := maxX-minX+1, maxY-minY+1
	at := func(x, y int) int32 { return grid[(minY+y)*size+minX+x] }
	for _, rc := range CraftingRecipes() {
		if rc.Shaped {
			if rc.W != w || rc.H != h {
				continue
			}
			for _, mirror := range []bool{false, true} {
				match := true
				for y := 0; y < h && match; y++ {
					for x := 0; x < w && match; x++ {
						gx := x
						if mirror {
							gx = w - 1 - x
						}
						match = accepts(rc.Cells[y*w+x], at(gx, y))
					}
				}
				if match {
					return rc, true
				}
			}
			continue
		}
		if len(rc.Cells) != filled {
			continue
		}
		var items []int32
		for _, id := range grid {
			if id >= 0 {
				items = append(items, id)
			}
		}
		if assign(rc.Cells, items, make([]bool, len(items))) {
			return rc, true
		}
	}
	return Recipe{}, false
}

// accepts reports whether a shaped cell takes id (-1 empty).
func accepts(cell []int32, id int32) bool {
	if cell == nil {
		return id < 0
	}
	for _, c := range cell {
		if c == id {
			return true
		}
	}
	return false
}

// assign reports whether each ingredient can take a different item.
func assign(ingredients [][]int32, items []int32, used []bool) bool {
	if len(ingredients) == 0 {
		return true
	}
	for i, id := range items {
		if !used[i] && accepts(ingredients[0], id) {
			used[i] = true
			if assign(ingredients[1:], items, used) {
				return true
			}
			used[i] = false
		}
	}
	return false
}

// String writes a recipe for test messages.
func (r Recipe) String() string {
	return "recipe for " + strconv.Itoa(int(r.Result)) + " x" + strconv.Itoa(r.Count)
}

// --- dimensions ---

// Dimension is a dimension type as a Java client is told it.
type Dimension struct {
	Name         string
	MinY, Height int
	SkyLight     bool
	SeaLevel     int32
}

var (
	dimensionsOnce sync.Once
	dimensions     map[string]Dimension
)

// DimensionType is a dimension type by name ("minecraft:the_nether"), from
// data/dimensions.gz: its height and sky light from minecraft-data's
// loginPacket.json (the dimension_type registry a 26.1 server sent), its sea
// level written by hand from minecraft.wiki "Terrain features" (oldid=3873202):
// 63, the Nether 31 and the End 0 (see tools/gentables' dimensions).
func DimensionType(name string) (Dimension, bool) {
	dimensionsOnce.Do(func() {
		dimensions = map[string]Dimension{}
		for _, l := range lines("dimensions") {
			f := fields(l)
			dimensions[f[0]] = Dimension{Name: f[0], MinY: atoi(f[1]), Height: atoi(f[2]), SkyLight: f[3] == "1", SeaLevel: int32(atoi(f[4]))}
		}
	})
	d, ok := dimensions[name]
	return d, ok
}

// craftRemainders are what an ingredient leaves in the crafting grid, on Java.
// minecraft-data's recipes have no remainders; these are minecraft.wiki's:
// "Cake" oldid=3861707 (the empty buckets "remain in the crafting grid") and
// "Honey Bottle" oldid=3753993 ("Empty bottles remain in the crafting grid"
// after crafting sugar and a honey block). They are the only remainders the
// crafting recipes above use.
var craftRemainders = map[string]string{
	"milk_bucket":  "bucket",
	"honey_bottle": "glass_bottle",
}

// CraftRemainder is the Java item an ingredient leaves in the grid when it is
// crafted with, "" for none. Names are Java's, with or without minecraft:.
func CraftRemainder(item string) string { return craftRemainders[strings.TrimPrefix(item, prefix)] }

// JavaItemName is the Java name of a Java item ID, without minecraft:, or "".
func JavaItemName(id int32) string {
	JavaItemID("")
	for n, i := range javaItemIDs {
		if i == id {
			return n
		}
	}
	return ""
}
