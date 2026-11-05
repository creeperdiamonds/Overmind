package structures

// Trial chambers: the br::worldgen jigsaw (brjigsaw.go, Java's placer) with the code-registered pools of
// pools_trial_chambers.go and the settings P11.7 read from the registration (0xd848460): start pool
// trial_chambers/chamber/end, maximum depth 20, start height uniform(-40, -20) (drawn first), no
// heightmap projection. Not read: the maximum distance, the start jigsaw and the pool aliases (the
// random spawner contents); Java's 116 is used for the distance, and jigsaws that name an alias find no
// pool and are skipped, as Java's placer skips a missing pool. HELD, not placed: against BDS's 13
// merged trial chamber volumes in the four worlds none is exact (the extents are close; Java's start
// jigsaw "minecraft:start" finds none), so the unread settings are not Java's (TestTrialChambersVolumes).
func (d *Data) trialPools() map[string]*pool {
	d.trialOnce.Do(func() {
		d.trial = map[string]*pool{"minecraft:empty": {}}
		els := map[string]*element{}
		for _, p := range trialChamberPools {
			pl := &pool{fallback: p.fallback}
			for _, e := range p.entries {
				var el *element
				if e.name != "" {
					if el = els[e.name]; el == nil {
						t := d.Template(e.name)
						if t == nil {
							d.problem("trial chamber element %s: no template", e.name)
							continue
						}
						el = &element{name: e.name, template: t}
						els[e.name] = el
					}
				}
				for range e.weight {
					pl.elems = append(pl.elems, el)
				}
			}
			d.trial[p.name] = pl
		}
	})
	return d.trial
}

// trialChambers assembles a start.
func (s *Structures) trialChambers(st Start) []*piece {
	return s.assembled.get(st, func(st Start) []*piece {
		r := largeFeatureRandom(s.seed, st.CX, st.CZ)
		y := -40 + int(r.NextInt(21)) // uniform(-40, -20), Java's UniformHeight.
		j := &jsonJigsaw{startPool: "minecraft:trial_chambers/chamber/end", maxDepth: 20, maxH: 116, maxV: 384, startY: y}
		pieces, _ := assembleJava(r, s.data.trialPools(), j, st.CX, st.CZ, s.g.SurfaceWG)
		return pieces
	})
}
