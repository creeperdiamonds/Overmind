package worldgen

import (
	"fmt"
	"math"
)

// The deep dark's sculk: sculk patches grown by charge cursors, and sculk veins. The study does not
// write these up, so the algorithms and parameters were written from knowledge of Java 26.1's sculk
// patch feature, its world generation spreader and the sculk vein's multiface spreader, and corrected
// against the study's trace fixtures (TestGoldenFeatureTraces). No Mojang code or file is in this repo.
// Ancient cities are structures and are not generated.

var (
	sSculk    = laterState("sculk")
	sCatalyst = laterState("sculk_catalyst[bloom=false]")
	sSensor   = laterState("sculk_sensor[power=0,sculk_sensor_phase=inactive,waterlogged=false]")
	sShrieker = laterState("sculk_shrieker[can_summon=true,shrieking=false,waterlogged=false]")
	// veinStates holds every sculk vein state, by its faces (bits in dir order) and water (bit 6).
	veinStates = func() (t [128]bs) {
		for i := range t {
			f := func(b int) bool { return i>>b&1 != 0 }
			t[i] = laterState(fmt.Sprintf("sculk_vein[down=%v,east=%v,north=%v,south=%v,up=%v,waterlogged=%v,west=%v]",
				f(int(dDown)), f(int(dEast)), f(int(dNorth)), f(int(dSouth)), f(int(dUp)), f(6), f(int(dWest))))
		}
		return t
	}()
	sculkVeinBlock = veinStates[0].block()
)

func isVein(s bs) bool { return s.block() == sculkVeinBlock }

// veinBits reads a vein's faces and water as veinStates' index.
func veinBits(s bs) int {
	if !isVein(s) {
		return 0
	}
	for i, v := range veinStates {
		if v == s {
			return i
		}
	}
	return 0
}

// sculkBehaviour says whether a block takes part in spreading: sculk and veins.
func sculkBehaviour(s bs) bool { return s == sSculk || isVein(s) }

func waterBlock(s bs) bool { return s.block() == sWater.block() }

// veinSpreadTypes are the multiface spreader's tries, in its order: the same block, the next block on
// the same plane, and around the edge.
type spreadPos struct {
	p    pos
	face dir
}

const (
	spreadSame = iota
	spreadPlane
	spreadWrap
)

func spreadAt(kind int, p pos, to, from dir) spreadPos {
	switch kind {
	case spreadSame:
		return spreadPos{p, to}
	case spreadPlane:
		return spreadPos{p.step(to), from}
	}
	return spreadPos{p.step(to).step(from), to.opposite()}
}

var (
	defaultSpread = []int{spreadSame, spreadPlane, spreadWrap}
	sameSpread    = []int{spreadSame}
)

// veinReplaceable is the sculk vein spreader's test of the block a face would go into.
func veinReplaceable(d *decoration, p, from pos, a dir, s bs) bool {
	behind := d.at(from.step(a))
	if behind == sSculk || behind.block() == sCatalyst.block() {
		return false
	}
	if absInt(p.x-from.x)+absInt(p.y-from.y)+absInt(p.z-from.z) == 2 {
		if sturdyFace(d.at(p.step(a.opposite())), a) {
			return false
		}
	}
	if s.hasFluid() && !s.has(fWater) {
		return false
	}
	if s.has(fReplaceable) {
		return true
	}
	return s.isAir() || isVein(s) || (waterBlock(s) && s.has(fSource))
}

func veinValidFor(d *decoration, s bs, p pos, a dir) bool {
	if isVein(s) && veinBits(s)>>a&1 != 0 {
		return false
	}
	return sturdyFace(d.at(p.step(a)), a.opposite())
}

func veinForPlacement(d *decoration, cur bs, p pos, a dir) (bs, bool) {
	if !veinValidFor(d, cur, p, a) {
		return 0, false
	}
	i := 0
	switch {
	case isVein(cur):
		i = veinBits(cur)
	case cur.has(fWater) && cur.has(fSource):
		i = 1 << 6
	}
	return veinStates[i|1<<a], true
}

// veinSpreadToward is one spread from a face toward a direction; it says whether a face was placed.
func veinSpreadToward(d *decoration, s bs, p pos, from, to dir, kinds []int) bool {
	if to.axis() == from.axis() {
		return false
	}
	if isVein(s) && !(veinBits(s)>>from&1 != 0 && veinBits(s)>>to&1 == 0) {
		return false
	}
	for _, k := range kinds {
		sp := spreadAt(k, p, to, from)
		cur := d.at(sp.p)
		if veinReplaceable(d, p, sp.p, sp.face, cur) && veinValidFor(d, cur, sp.p, sp.face) {
			v, ok := veinForPlacement(d, cur, sp.p, sp.face)
			if !ok || !d.canWrite(sp.p.x, sp.p.y, sp.p.z) {
				return false
			}
			d.set(sp.p.x, sp.p.y, sp.p.z, v)
			return true
		}
	}
	return false
}

// veinSpreadAll spreads from every face of a block (every side, for a block that is not a vein).
func veinSpreadAll(d *decoration, s bs, p pos, kinds []int) int {
	n := 0
	for _, from := range allDirs {
		if isVein(s) && veinBits(s)>>from&1 == 0 {
			continue
		}
		for _, to := range allDirs {
			if to == from || to == from.opposite() {
				continue
			}
			if veinSpreadToward(d, s, p, from, to, kinds) {
				n++
			}
		}
	}
	return n
}

func veinSpreadRandom(d *decoration, s bs, p pos, from dir) {
	for _, to := range shuffledDirs(&d.r, allDirs[:]) {
		if veinSpreadToward(d, s, p, from, to, defaultSpread) {
			return
		}
	}
}

// veinFeature is multiface_growth for sculk veins.
func veinFeature(on map[uint16]bool, search int, chance float32) feature {
	placeable := []dir{dUp, dDown, dNorth, dEast, dSouth, dWest}
	airOrWater := func(s bs) bool { return s.isAir() || waterBlock(s) }
	return func(d *decoration, o pos) bool {
		r := &d.r
		start := d.at(o)
		if !airOrWater(start) {
			return false
		}
		put := func(q pos, old bs, dirs []dir) bool {
			for _, a := range dirs {
				if on[stateBlock[d.at(q.step(a))]] {
					v, ok := veinForPlacement(d, old, q, a)
					if !ok {
						return false
					}
					d.set(q.x, q.y, q.z, v)
					if r.nextFloat() < chance {
						veinSpreadRandom(d, v, q, a)
					}
					return true
				}
			}
			return false
		}
		dirs := shuffledDirs(r, placeable)
		if put(o, start, dirs) {
			return true
		}
		for _, sd := range dirs {
			var pd []dir
			for _, x := range placeable {
				if x != sd.opposite() {
					pd = append(pd, x)
				}
			}
			pd = shuffledDirs(r, pd)
			for range search {
				q := o.step(sd) // The game sets it from the origin each time.
				b := d.at(q)
				if !airOrWater(b) && !isVein(b) {
					break
				}
				if put(q, b, pd) {
					return true
				}
			}
		}
		return false
	}
}

// The world generation spreader's numbers.
const (
	sculkGrowthCost   = 50
	sculkNoGrowth     = 1
	sculkChargeDecay  = 5
	sculkExtraDecay   = 10
	sculkMaxCursors   = 32
	sculkCursorCharge = 1000
)

type chargeCursor struct {
	p          pos
	charge     int
	delay      int
	decay      int
	facings    int // Face bits; -1 when unset.
	hasFacings bool
}

// cornerless are the 18 neighbours that share a face or an edge, in the game's order: x fastest, then
// y, then z.
var cornerless = func() (l []pos) {
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if (x == 0 || y == 0 || z == 0) && !(x == 0 && y == 0 && z == 0) {
					l = append(l, pos{x, y, z})
				}
			}
		}
	}
	return
}()

// sculkPatch is the sculk_patch feature.
type sculkPatch struct {
	charges, perCharge, attempts, growthRounds, spreadRounds int
	catalyst                                                 float32
}

func fullCube(s bs) bool {
	return s.has(fMotion) && s.has(fSturdyUp) && s.has(fSturdyDown) && s.has(fSturdySide)
}

func (c *sculkPatch) feature(d *decoration, o pos) bool {
	if !sculkCanSpreadFrom(d, o) {
		return false
	}
	r := &d.r
	var cursors []*chargeCursor
	for round := range c.spreadRounds + c.growthRounds {
		for range c.charges {
			for left := c.perCharge; left > 0; {
				n := min(left, sculkCursorCharge)
				if len(cursors) < sculkMaxCursors {
					cursors = append(cursors, &chargeCursor{p: o, charge: n, decay: 1})
				}
				left -= n
			}
		}
		veins := round < c.spreadRounds
		for range c.attempts {
			cursors = sculkUpdate(d, o, cursors, veins)
		}
		cursors = cursors[:0]
	}
	below := o.step(dDown)
	if r.nextFloat() <= c.catalyst && fullCube(d.at(below)) {
		d.set(o.x, o.y, o.z, sCatalyst)
	}
	return true
}

func sculkCanSpreadFrom(d *decoration, p pos) bool {
	s := d.at(p)
	if sculkBehaviour(s) {
		return true
	}
	if !s.isAir() && !(waterBlock(s) && s.has(fSource)) {
		return false
	}
	for _, a := range allDirs {
		if fullCube(d.at(p.step(a))) {
			return true
		}
	}
	return false
}

func sculkUpdate(d *decoration, o pos, list []*chargeCursor, veins bool) []*chargeCursor {
	if len(list) == 0 {
		return list
	}
	var next []*chargeCursor
	merged := map[pos]*chargeCursor{}
	for _, c := range list {
		c.update(d, o, veins)
		if c.charge <= 0 {
			continue
		}
		m := merged[c.p]
		if m == nil {
			merged[c.p] = c
			next = append(next, c)
		} else {
			next = append(next, c)
			if c.charge < m.charge {
				merged[c.p] = c
			}
		}
	}
	return next
}

func (c *chargeCursor) update(d *decoration, o pos, veins bool) {
	if c.charge <= 0 {
		return
	}
	if c.delay > 0 {
		c.delay--
		return
	}
	s := d.at(c.p)
	if veins && sculkSpreadVein(d, c, s) {
		if s != sSculk { // Every behaviour but sculk's re-reads the block after spreading.
			s = d.at(c.p)
		}
	}
	c.charge = sculkUseCharge(d, c, s, o, veins)
	if c.charge <= 0 {
		sculkDischarged(d, s, c.p)
		return
	}
	next, ok := sculkMove(d, c.p)
	if ok {
		sculkDischarged(d, s, c.p)
		c.p = next
		dx, dz := float64(c.p.x-o.x), float64(c.p.z-o.z)
		if dx*dx+dz*dz >= 15*15 {
			c.charge = 0
			return
		}
	}
	// The decay and delay come from the behaviour of the block the cursor was on.
	if !sculkBehaviour(s) {
		c.decay = max(c.decay-1, 0)
	} else {
		c.decay = 1
	}
	c.delay = 1
	// Faces come from the block moved to, or from the block as it was read before charge was used.
	ns := s
	if ok {
		ns = d.at(c.p)
	}
	if sculkBehaviour(ns) {
		c.hasFacings = true
		c.facings = veinBits(ns) & 63
	}
}

// sculkSpreadVein is a block's vein spreading as the cursor arrives.
func sculkSpreadVein(d *decoration, c *chargeCursor, s bs) bool {
	if sculkBehaviour(s) {
		return veinSpreadAll(d, s, c.p, defaultSpread) > 0
	}
	if !c.hasFacings {
		return veinSpreadAll(d, d.at(c.p), c.p, sameSpread) > 0
	}
	if c.facings != 0 {
		if !s.isAir() && !s.has(fWater) {
			return false
		}
		return veinRegrow(d, c.p, s, c.facings)
	}
	return veinSpreadAll(d, s, c.p, defaultSpread) > 0
}

func veinRegrow(d *decoration, p pos, s bs, faces int) bool {
	i := 0
	for _, a := range allDirs {
		if faces>>a&1 != 0 && sturdyFace(d.at(p.step(a)), a.opposite()) {
			i |= 1 << a
		}
	}
	if i == 0 {
		return false
	}
	if s.hasFluid() {
		i |= 1 << 6
	}
	d.set(p.x, p.y, p.z, veinStates[i])
	return true
}

func sculkUseCharge(d *decoration, c *chargeCursor, s bs, o pos, veins bool) int {
	r := &d.r
	switch {
	case s == sSculk:
		charge := c.charge
		if charge == 0 || r.nextInt(sculkChargeDecay) != 0 {
			return charge
		}
		p := c.p
		dsq := float64(p.x-o.x)*float64(p.x-o.x) + float64(p.y-o.y)*float64(p.y-o.y) + float64(p.z-o.z)*float64(p.z-o.z)
		near := dsq < sculkNoGrowth*sculkNoGrowth
		if !near && sculkCanGrow(d, p) {
			if r.nextInt(sculkGrowthCost) < charge {
				up := p.step(dUp)
				g := sSensor
				if r.nextInt(11) == 0 {
					g = sShrieker
				}
				if d.at(up).hasFluid() {
					g = withProp(g, "waterlogged", "true")
				}
				d.set(up.x, up.y, up.z, g)
			}
			return max(0, charge-sculkGrowthCost)
		}
		if r.nextInt(sculkExtraDecay) != 0 {
			return charge
		}
		if near {
			return charge - 1
		}
		a := float32(float32(math.Sqrt(dsq)) - sculkNoGrowth)
		a = float32(a * a)
		f := min(1, a/float32((24-sculkNoGrowth)*(24-sculkNoGrowth)))
		return charge - max(1, int(float32(float32(float32(charge)*f)*0.5)))
	case isVein(s):
		if veins && sculkPlace(d, c.p, s) {
			return c.charge - 1
		}
		if r.nextInt(sculkChargeDecay) == 0 {
			return int(math.Floor(float64(float32(float32(c.charge) * 0.5))))
		}
		return c.charge
	}
	if c.decay > 0 {
		return c.charge
	}
	return 0
}

func sculkCanGrow(d *decoration, p pos) bool {
	up := d.at(p.step(dUp))
	if !up.isAir() && !(waterBlock(up) && up.has(fWater)) {
		return false
	}
	n := 0
	for y := p.y; y <= p.y+2; y++ {
		for z := p.z - 4; z <= p.z+4; z++ {
			for x := p.x - 4; x <= p.x+4; x++ {
				b := d.at(pos{x, y, z}).block()
				if b == sSensor.block() || b == sShrieker.block() {
					n++
				}
				if n > 2 {
					return false
				}
			}
		}
	}
	return true
}

// sculkPlace turns the block behind one of a vein's faces to sculk.
func sculkPlace(d *decoration, p pos, s bs) bool {
	for _, a := range shuffledDirs(&d.r, allDirs[:]) {
		if veinBits(s)>>a&1 == 0 {
			continue
		}
		q := p.step(a)
		if !d.at(q).tagged(tagSculkSwap) {
			continue
		}
		d.set(q.x, q.y, q.z, sSculk)
		veinSpreadAll(d, sSculk, q, defaultSpread)
		back := a.opposite()
		for _, b := range allDirs {
			if b == back {
				continue
			}
			n := q.step(b)
			if ns := d.at(n); isVein(ns) {
				sculkDischarged(d, ns, n)
			}
		}
		return true
	}
	return false
}

// sculkDischarged is a vein letting go of the faces that now rest on sculk.
func sculkDischarged(d *decoration, s bs, p pos) {
	if !isVein(s) {
		return
	}
	i := veinBits(s)
	for _, a := range allDirs {
		if i>>a&1 != 0 && d.at(p.step(a)) == sSculk {
			i &^= 1 << a
		}
	}
	v := veinStates[i]
	if i&63 == 0 {
		v = sAir
		if s.has(fWater) {
			v = sWater
		}
	}
	d.set(p.x, p.y, p.z, v)
}

func sculkMove(d *decoration, p pos) (pos, bool) {
	r := &d.r
	list := append([]pos{}, cornerless...)
	for i := len(list); i > 1; i-- {
		j := r.nextInt(i)
		list[i-1], list[j] = list[j], list[i-1]
	}
	best := p
	for _, off := range list {
		n := p.add(off.x, off.y, off.z)
		s := d.at(n)
		if sculkBehaviour(s) && sculkUnobstructed(d, p, n) {
			best = n
			if veinOnSubstrate(d, s, n) {
				break
			}
		}
	}
	return best, best != p
}

func veinOnSubstrate(d *decoration, s bs, p pos) bool {
	if !isVein(s) {
		return false
	}
	for _, a := range allDirs {
		if veinBits(s)>>a&1 != 0 && d.at(p.step(a)).tagged(tagSculkSwap) {
			return true
		}
	}
	return false
}

func sculkUnobstructed(d *decoration, from, to pos) bool {
	dx, dy, dz := to.x-from.x, to.y-from.y, to.z-from.z
	if absInt(dx)+absInt(dy)+absInt(dz) == 1 {
		return true
	}
	pick := func(v int, neg, posd dir) dir {
		if v < 0 {
			return neg
		}
		return posd
	}
	ax, ay, az := pick(dx, dWest, dEast), pick(dy, dDown, dUp), pick(dz, dNorth, dSouth)
	open := func(a dir) bool { return !sturdyFace(d.at(from.step(a)), a.opposite()) }
	switch {
	case dx == 0:
		return open(ay) || open(az)
	case dy == 0:
		return open(ax) || open(az)
	}
	return open(ax) || open(ay)
}

func defineSculk(t *tableBuilder) {
	patch := &sculkPatch{charges: 10, perCharge: 32, attempts: 64, growthRounds: 0, spreadRounds: 1, catalyst: 0.5}
	t.def("sculk_patch_deep_dark", patch.feature, countMod{constInt(256)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, biomeMod{})
	on := blockSet("stone andesite diorite granite dripstone_block calcite tuff deepslate")
	t.def("sculk_vein", veinFeature(on, 20, 1), countMod{uniInt(204, 250)}, inSquareMod{}, heightRangeMod{uniHeight(minY, 256)}, biomeMod{})
}
