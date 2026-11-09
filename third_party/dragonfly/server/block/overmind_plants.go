package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Overmind fork: the plants of 1.21.5 and later that Overmind's world generator places and Dragonfly
// had no Go type for (bush, firefly bush, short and tall dry grass, cactus flower, leaf litter,
// wildflowers). Without a type they were registered states only, so a client that breaks them at once
// (all have hardness 0) saw them come back. Hardness, tools, drops and the ground each stands on are
// the game's, from public game knowledge; nothing here is Mojang code.

var (
	hashBush         = NextHash()
	hashFireflyBush  = NextHash()
	hashShortDry     = NextHash()
	hashTallDry      = NextHash()
	hashCactusFlower = NextHash()
	hashLeafLitter   = NextHash()
	hashWildflowers  = NextHash()
)

// vegetationGround lists the blocks the bush family stands on: the dirt family and farmland (the game's
// supports_vegetation), by name so Overmind's own blocks (moss, rooted dirt) count too.
var vegetationGround = map[string]bool{
	"minecraft:grass_block": true, "minecraft:dirt": true, "minecraft:coarse_dirt": true, "minecraft:podzol": true,
	"minecraft:mycelium": true, "minecraft:dirt_with_roots": true, "minecraft:moss_block": true, "minecraft:pale_moss_block": true,
	"minecraft:mud": true, "minecraft:muddy_mangrove_roots": true, "minecraft:farmland": true,
}

// dryGround lists the blocks dry plants stand on: the dirt family, sand and terracotta (the game's
// supports_dry_vegetation).
var dryGround = func() map[string]bool {
	m := map[string]bool{"minecraft:sand": true, "minecraft:red_sand": true, "minecraft:suspicious_sand": true, "minecraft:hardened_clay": true}
	for k := range vegetationGround {
		m[k] = true
	}
	for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
		m["minecraft:"+c+"_terracotta"] = true
	}
	return m
}()

func groundIn(set map[string]bool, b world.Block) bool {
	name, _ := b.EncodeBlock()
	return set[name]
}

// sturdyBelow says whether the block under pos has a full top face.
func sturdyBelow(pos cube.Pos, tx *world.Tx) bool {
	down := pos.Side(cube.FaceDown)
	return tx.Block(down).Model().FaceSolid(down, cube.FaceUp, tx)
}

// plantSurvives is a plant's ground rule.
type plantSurvives func(pos cube.Pos, tx *world.Tx) bool

func onGround(set map[string]bool) plantSurvives {
	return func(pos cube.Pos, tx *world.Tx) bool { return groundIn(set, tx.Block(pos.Side(cube.FaceDown))) }
}

// placePlant places a plant from its item where its ground rule holds.
func placePlant(b world.Block, ok plantSurvives, pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used || !ok(pos, tx) {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// shearsOnly drops the item only to shears or silk touch.
func shearsOnly(it world.Item, n int) func(item.Tool, []item.Enchantment) []item.Stack {
	return func(t item.Tool, e []item.Enchantment) []item.Stack {
		if t.ToolType() == item.TypeShears || hasSilkTouch(e) {
			return []item.Stack{item.NewStack(it, n)}
		}
		return nil
	}
}

// Bush is a leafy plant of forests and plains. Any block replaces it; it drops itself only to shears.
type Bush struct {
	replaceable
	transparent
	empty
	sourceWaterDisplacer
}

// BreakInfo ...
func (b Bush) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, shearsEffective, shearsOnly(b, 1))
}

// FlammabilityInfo ...
func (Bush) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, false) }

// CompostChance ...
func (Bush) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (Bush) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick ...
func (b Bush) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onGround(vegetationGround)(pos, tx) {
		breakBlock(b, pos, tx)
	}
}

// UseOnBlock ...
func (b Bush) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placePlant(b, onGround(vegetationGround), pos, face, tx, user, ctx)
}

// EncodeItem ...
func (Bush) EncodeItem() (string, int16) { return "minecraft:bush", 0 }

// EncodeBlock ...
func (Bush) EncodeBlock() (string, map[string]any) { return "minecraft:bush", nil }

// Hash ...
func (Bush) Hash() (uint64, uint64) { return hashBush, 0 }

// FireflyBush is a bush of swamps and riversides that glows faintly. It drops itself.
type FireflyBush struct {
	transparent
	empty
	sourceWaterDisplacer
}

// BreakInfo ...
func (b FireflyBush) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(b))
}

// LightEmissionLevel ...
func (FireflyBush) LightEmissionLevel() uint8 { return 2 }

// FlammabilityInfo ...
func (FireflyBush) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, false) }

// CompostChance ...
func (FireflyBush) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (FireflyBush) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick ...
func (b FireflyBush) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onGround(vegetationGround)(pos, tx) {
		breakBlock(b, pos, tx)
	}
}

// UseOnBlock ...
func (b FireflyBush) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placePlant(b, onGround(vegetationGround), pos, face, tx, user, ctx)
}

// EncodeItem ...
func (FireflyBush) EncodeItem() (string, int16) { return "minecraft:firefly_bush", 0 }

// EncodeBlock ...
func (FireflyBush) EncodeBlock() (string, map[string]any) { return "minecraft:firefly_bush", nil }

// Hash ...
func (FireflyBush) Hash() (uint64, uint64) { return hashFireflyBush, 0 }

// ShortDryGrass is a dry plant of deserts and badlands. Any block replaces it; it drops itself only to
// shears.
type ShortDryGrass struct {
	replaceable
	transparent
	empty
	sourceWaterDisplacer
}

// BreakInfo ...
func (g ShortDryGrass) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, shearsEffective, shearsOnly(g, 1))
}

// FlammabilityInfo ...
func (ShortDryGrass) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, false) }

// CompostChance ...
func (ShortDryGrass) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (ShortDryGrass) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick ...
func (g ShortDryGrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onGround(dryGround)(pos, tx) {
		breakBlock(g, pos, tx)
	}
}

// UseOnBlock ...
func (g ShortDryGrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placePlant(g, onGround(dryGround), pos, face, tx, user, ctx)
}

// EncodeItem ...
func (ShortDryGrass) EncodeItem() (string, int16) { return "minecraft:short_dry_grass", 0 }

// EncodeBlock ...
func (ShortDryGrass) EncodeBlock() (string, map[string]any) { return "minecraft:short_dry_grass", nil }

// Hash ...
func (ShortDryGrass) Hash() (uint64, uint64) { return hashShortDry, 0 }

// TallDryGrass is the taller dry grass, one block high. Any block replaces it; it drops itself only to
// shears.
type TallDryGrass struct {
	replaceable
	transparent
	empty
	sourceWaterDisplacer
}

// BreakInfo ...
func (g TallDryGrass) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, shearsEffective, shearsOnly(g, 1))
}

// FlammabilityInfo ...
func (TallDryGrass) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, false) }

// CompostChance ...
func (TallDryGrass) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (TallDryGrass) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick ...
func (g TallDryGrass) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onGround(dryGround)(pos, tx) {
		breakBlock(g, pos, tx)
	}
}

// UseOnBlock ...
func (g TallDryGrass) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placePlant(g, onGround(dryGround), pos, face, tx, user, ctx)
}

// EncodeItem ...
func (TallDryGrass) EncodeItem() (string, int16) { return "minecraft:tall_dry_grass", 0 }

// EncodeBlock ...
func (TallDryGrass) EncodeBlock() (string, map[string]any) { return "minecraft:tall_dry_grass", nil }

// Hash ...
func (TallDryGrass) Hash() (uint64, uint64) { return hashTallDry, 0 }

// CactusFlower grows on top of a cactus. It drops itself.
type CactusFlower struct {
	transparent
	empty
	sourceWaterDisplacer
}

// cactusFlowerSurvives: on a cactus, farmland or a block with a full top face.
func cactusFlowerSurvives(pos cube.Pos, tx *world.Tx) bool {
	switch tx.Block(pos.Side(cube.FaceDown)).(type) {
	case Cactus, Farmland:
		return true
	}
	return sturdyBelow(pos, tx)
}

// BreakInfo ...
func (f CactusFlower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(f))
}

// CompostChance ...
func (CactusFlower) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (CactusFlower) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick ...
func (f CactusFlower) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !cactusFlowerSurvives(pos, tx) {
		breakBlock(f, pos, tx)
	}
}

// UseOnBlock ...
func (f CactusFlower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placePlant(f, cactusFlowerSurvives, pos, face, tx, user, ctx)
}

// EncodeItem ...
func (CactusFlower) EncodeItem() (string, int16) { return "minecraft:cactus_flower", 0 }

// EncodeBlock ...
func (CactusFlower) EncodeBlock() (string, map[string]any) { return "minecraft:cactus_flower", nil }

// Hash ...
func (CactusFlower) Hash() (uint64, uint64) { return hashCactusFlower, 0 }

// LeafLitter is fallen leaves lying on a block. Any block replaces it.
type LeafLitter struct {
	replaceable
	transparent
	empty

	// AdditionalCount is the number of pieces past the first, 0 to 3 (Bedrock's growth state runs to 7).
	AdditionalCount int
	// Facing is the direction the pieces face.
	Facing cube.Direction
}

// BreakInfo ...
func (l LeafLitter) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(LeafLitter{}, min(l.AdditionalCount, 3)+1)))
}

// FlammabilityInfo ...
func (LeafLitter) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, true) }

// CompostChance ...
func (LeafLitter) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (LeafLitter) HasLiquidDrops() bool { return true }

// NeighbourUpdateTick: leaf litter needs a full top face under it.
func (l LeafLitter) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !sturdyBelow(pos, tx) {
		breakBlock(l, pos, tx)
	}
}

// UseOnBlock adds a piece to leaf litter already there, or places new litter.
func (l LeafLitter) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(LeafLitter); ok {
		if existing.AdditionalCount >= 3 {
			return false
		}
		existing.AdditionalCount++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}
	l.Facing = user.Rotation().Direction().Opposite()
	return placePlant(l, sturdyBelow, pos, face, tx, user, ctx)
}

// EncodeItem ...
func (LeafLitter) EncodeItem() (string, int16) { return "minecraft:leaf_litter", 0 }

// EncodeBlock ...
func (l LeafLitter) EncodeBlock() (string, map[string]any) {
	return "minecraft:leaf_litter", map[string]any{"growth": int32(l.AdditionalCount), "minecraft:cardinal_direction": l.Facing.String()}
}

// Hash ...
func (l LeafLitter) Hash() (uint64, uint64) {
	return hashLeafLitter, uint64(l.AdditionalCount) | uint64(l.Facing)<<8
}

// Wildflowers are small flowers lying on the ground in birch forests and meadows.
type Wildflowers struct {
	transparent
	empty

	// AdditionalCount is the number of flowers past the first, 0 to 3 (Bedrock's growth state runs to 7).
	AdditionalCount int
	// Facing is the direction the flowers face.
	Facing cube.Direction
}

// BreakInfo ...
func (w Wildflowers) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(Wildflowers{}, min(w.AdditionalCount, 3)+1)))
}

// FlammabilityInfo ...
func (Wildflowers) FlammabilityInfo() FlammabilityInfo { return newFlammabilityInfo(60, 100, true) }

// CompostChance ...
func (Wildflowers) CompostChance() float64 { return 0.3 }

// HasLiquidDrops ...
func (Wildflowers) HasLiquidDrops() bool { return true }

// BoneMeal adds a flower, or drops one when the block is full, as pink petals.
func (w Wildflowers) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if w.AdditionalCount < 3 {
		w.AdditionalCount++
		tx.SetBlock(pos, w, nil)
		return item.BoneMealResultSmall
	}
	dropItem(tx, item.NewStack(Wildflowers{}, 1), pos.Vec3Centre())
	return item.BoneMealResultSmall
}

// NeighbourUpdateTick ...
func (w Wildflowers) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onGround(vegetationGround)(pos, tx) {
		breakBlock(w, pos, tx)
	}
}

// UseOnBlock adds a flower to wildflowers already there, or places new ones.
func (w Wildflowers) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(Wildflowers); ok {
		if existing.AdditionalCount >= 3 {
			return false
		}
		existing.AdditionalCount++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}
	w.Facing = user.Rotation().Direction().Opposite()
	return placePlant(w, onGround(vegetationGround), pos, face, tx, user, ctx)
}

// EncodeItem ...
func (Wildflowers) EncodeItem() (string, int16) { return "minecraft:wildflowers", 0 }

// EncodeBlock ...
func (w Wildflowers) EncodeBlock() (string, map[string]any) {
	return "minecraft:wildflowers", map[string]any{"growth": int32(w.AdditionalCount), "minecraft:cardinal_direction": w.Facing.String()}
}

// Hash ...
func (w Wildflowers) Hash() (uint64, uint64) {
	return hashWildflowers, uint64(w.AdditionalCount) | uint64(w.Facing)<<8
}

// registerOvermindPlants registers the plants above, every state, and their items.
func registerOvermindPlants() {
	for _, b := range []world.Block{Bush{}, FireflyBush{}, ShortDryGrass{}, TallDryGrass{}, CactusFlower{}} {
		world.RegisterBlock(b)
		world.RegisterItem(b.(world.Item))
	}
	for i := 0; i <= 7; i++ {
		for _, d := range cube.Directions() {
			world.RegisterBlock(LeafLitter{AdditionalCount: i, Facing: d})
			world.RegisterBlock(Wildflowers{AdditionalCount: i, Facing: d})
		}
	}
	world.RegisterItem(LeafLitter{})
	world.RegisterItem(Wildflowers{})
}

func init() { registerOvermindPlants() }
