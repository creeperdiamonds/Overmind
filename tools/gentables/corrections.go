package main

import "strings"

// recipeCorrection adds recipes where minecraft-data's recipes.json leaves
// out items the game accepts: minecraft-data lists one item where the game
// takes a tag (planks from the log only, not its wood or stripped forms) or
// leaves out some of a list (re-dyeing from one colour only). The lines, in
// the recipes table's format, are added beside minecraft-data's narrower
// recipe, which then is a subset of the corrected one. Each names its source.
type recipeCorrection struct {
	result string
	source string
	lines  []string
}

// The colours of dyed items, as Java names them.
var dyeColours = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

var recipeCorrections = func() []recipeCorrection {
	var out []recipeCorrection
	// minecraft.wiki "Planks" oldid=3833513: 4 planks from "Matching Log or
	// Matching Stem or Matching Stripped Log or Matching Stripped Stem" or
	// "Matching Wood or Matching Hyphae or Matching Stripped Wood or Matching
	// Stripped Hyphae"; 2 from "Block of Bamboo or Block of Stripped Bamboo".
	planks := "minecraft.wiki Planks oldid=3833513"
	for _, w := range []string{"oak", "spruce", "birch", "jungle", "acacia", "dark_oak", "mangrove", "cherry", "pale_oak"} {
		out = append(out, recipeCorrection{w + "_planks", planks, []string{
			"shapeless\t" + w + "_planks\t4\t" + strings.Join([]string{w + "_log", w + "_wood", "stripped_" + w + "_log", "stripped_" + w + "_wood"}, "|")}})
	}
	for _, w := range []string{"crimson", "warped"} {
		out = append(out, recipeCorrection{w + "_planks", planks, []string{
			"shapeless\t" + w + "_planks\t4\t" + strings.Join([]string{w + "_stem", w + "_hyphae", "stripped_" + w + "_stem", "stripped_" + w + "_hyphae"}, "|")}})
	}
	out = append(out, recipeCorrection{"bamboo_planks", planks, []string{"shapeless\tbamboo_planks\t2\tbamboo_block|stripped_bamboo_block"}})

	// Re-dyeing: one item of any other colour and one dye make one of the
	// dye's colour. minecraft.wiki: Wool oldid=3851387 ("Wool of any color can
	// be re-dyed into any other color"), Carpet oldid=3870641 ("Carpets of any
	// color can now be dyed into any other color ... one piece of dye can only
	// dye one carpet"), Bed oldid=3862101 ("A bed of any color can be re-dyed
	// using dyes"), Harness oldid=3785441 ("Any Harness + Matching Dye").
	for _, thing := range []struct{ name, source string }{
		{"wool", "minecraft.wiki Wool oldid=3851387"}, {"carpet", "minecraft.wiki Carpet oldid=3870641"},
		{"bed", "minecraft.wiki Bed oldid=3862101"}, {"harness", "minecraft.wiki Harness oldid=3785441"},
	} {
		for _, c := range dyeColours {
			var others []string
			for _, o := range dyeColours {
				if o != c {
					others = append(others, o+"_"+thing.name)
				}
			}
			out = append(out, recipeCorrection{c + "_" + thing.name, thing.source, []string{
				"shapeless\t" + c + "_" + thing.name + "\t1\t" + c + "_dye\t" + strings.Join(others, "|")}})
		}
	}
	// minecraft.wiki Pumpkin Pie oldid=3822422: Pumpkin, Sugar and "Any Egg"
	// ("Pumpkin pie can now be crafted using blue eggs or brown eggs").
	out = append(out, recipeCorrection{"pumpkin_pie", "minecraft.wiki Pumpkin Pie oldid=3822422", []string{
		"shapeless\tpumpkin_pie\t1\tpumpkin\tsugar\tblue_egg|brown_egg|egg"}})
	// minecraft.wiki Fire Charge oldid=3779438: "Blaze Powder + Coal or Charcoal
	// + Gunpowder", three charges.
	out = append(out, recipeCorrection{"fire_charge", "minecraft.wiki Fire Charge oldid=3779438", []string{
		"shapeless\tfire_charge\t3\tgunpowder\tblaze_powder\tcharcoal|coal"}})
	return out
}()
