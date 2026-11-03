package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// mdRecipe is one recipe in minecraft-data's recipes.json: a shape of item
// IDs (null for an empty cell) or a list of ingredients, and the result.
type mdRecipe struct {
	InShape     [][]*int32 `json:"inShape"`
	Ingredients []int32    `json:"ingredients"`
	Result      struct {
		ID    int32 `json:"id"`
		Count int   `json:"count"`
	} `json:"result"`
}

// recipeKey groups the variants minecraft-data lists for one Java recipe.
type recipeKey struct {
	shaped        bool
	w, h          int
	result, count string
	shape         string // which cells are empty, for shaped recipes
}

// recipes writes the crafting table from minecraft-data's 26.1 recipes.json,
// in lines of
//
//	shaped<TAB>width<TAB>height<TAB>result<TAB>count<TAB>cell...
//	shapeless<TAB>result<TAB>count<TAB>ingredient...
//
// where a cell or ingredient is the item names it accepts, joined by "|" ("-"
// for an empty cell), without the minecraft: prefix.
//
// minecraft-data lists a recipe whose ingredient is a tag or a list once per
// item, all of one item at a time (sticks from oak planks, sticks from spruce
// planks). Variants with the same result, count and shape are merged into one
// recipe whose cells accept every item they were listed with, which is the
// game's rule: any item of the ingredient in each cell. minecraft-data also
// drops some tag members altogether (planks only from the log itself);
// recipeCorrections, each cited, put those back.
func (g *gen) recipes() {
	var raw map[string][]mdRecipe
	readJSON(filepath.Join(g.pc, "recipes.json"), &raw)
	var items []item
	readJSON(filepath.Join(g.pc, "items.json"), &items)
	name := map[int32]string{}
	for _, it := range items {
		name[it.ID] = it.Name
	}
	type merged struct {
		key   recipeKey
		cells []map[string]bool
	}
	byKey := map[recipeKey]*merged{}
	var order []recipeKey
	results := make([]string, 0, len(raw))
	for k := range raw {
		results = append(results, k)
	}
	sort.Slice(results, func(i, j int) bool { a, _ := strconv.Atoi(results[i]); b, _ := strconv.Atoi(results[j]); return a < b })
	for _, k := range results {
		for _, r := range raw[k] {
			key := recipeKey{result: name[r.Result.ID], count: strconv.Itoa(max(r.Result.Count, 1))}
			var cells [][]string
			if r.InShape != nil {
				key.shaped = true
				key.h = len(r.InShape)
				for _, row := range r.InShape {
					key.w = max(key.w, len(row))
				}
				for _, row := range r.InShape {
					for x := 0; x < key.w; x++ {
						if x >= len(row) || row[x] == nil {
							key.shape += "-"
							cells = append(cells, nil)
							continue
						}
						key.shape += "#"
						cells = append(cells, []string{name[*row[x]]})
					}
				}
			} else {
				key.w = len(r.Ingredients) // only to keep arities apart
				for _, id := range r.Ingredients {
					cells = append(cells, []string{name[id]})
				}
			}
			m, ok := byKey[key]
			if !ok {
				m = &merged{key: key, cells: make([]map[string]bool, len(cells))}
				for i := range m.cells {
					m.cells[i] = map[string]bool{}
				}
				byKey[key] = m
				order = append(order, key)
			}
			for i, c := range cells {
				for _, n := range c {
					m.cells[i][n] = true
				}
			}
		}
	}
	lines := map[string]bool{}
	var out []string
	add := func(l string) {
		if !lines[l] {
			lines[l] = true
			out = append(out, l)
		}
	}
	for _, key := range order {
		m := byKey[key]
		cell := func(set map[string]bool) string {
			if len(set) == 0 {
				return "-"
			}
			names := make([]string, 0, len(set))
			for n := range set {
				names = append(names, n)
			}
			sort.Strings(names)
			return strings.Join(names, "|")
		}
		var l string
		if key.shaped {
			l = fmt.Sprintf("shaped\t%d\t%d\t%s\t%s", key.w, key.h, key.result, key.count)
		} else {
			l = "shapeless\t" + key.result + "\t" + key.count
		}
		for _, set := range m.cells {
			l += "\t" + cell(set)
		}
		add(l)
	}
	for _, c := range recipeCorrections {
		for _, l := range c.lines {
			add(l)
		}
	}
	slices.Sort(out)
	saved := g.header
	g.header += "; with tools/gentables/corrections.go (minecraft.wiki, cited per entry)"
	g.writeLines("recipes", out)
	g.header = saved
	fmt.Printf("recipes: %d crafting (%d corrections)\n", len(out), len(recipeCorrections))
}
