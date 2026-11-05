package worldgen

// Ancient debris, steps 14 and 15 of the Nether's decoration (NOTES-bedrock-nether N5 step table and
// (G); counts and the skipped words from NOTES-bedrock-exact T28, read from the code): after the
// data-driven passes, the chunk's MT is seeded again with the chunk seed, one word is used up, and one
// more (basalt_pillar_feature's rule-level Math.Random, a nextFloat) when every quart of the chunk is
// soul sand valley. Then 2 deep blobs at y 8..23 of nextInt(4) blocks and 3 tall ones at y 8..119 of
// nextInt(3), each NoSurfaceOreFeature (0x5409930): a random walk from the position. The study's
// figures: seed 1 98.88% / 98.88% recall / precision of BDS's ancient debris (196 chunks), seed 100's
// soul sand and basalt delta boxes 100%. Which call uses the first word is not read.

var sAncientDebris = laterState("ancient_debris")

// netherDebris places the chunk's ancient debris on r, reseeded here.
func (d *decoration) netherDebris(r *bmt, cx, cz int) {
	r.setSeed(d.g.bseeds.chunk(cx, cz))
	r.u32()
	if d.g.netherN.singleBiomeChunk(cx, cz, bSoulSandValley) {
		r.nextFloat()
	}
	x0, z0 := cx*16, cz*16
	for range 2 {
		x, y, z := int(r.nextIntN(16))+x0, int(r.nextIntN(16))+8, int(r.nextIntN(16))+z0
		d.noSurfaceOre(r, x, y, z, 0, 4)
	}
	for range 3 {
		x, y, z := int(r.nextIntN(16))+x0, int(r.nextIntN(112))+8, int(r.nextIntN(16))+z0
		d.noSurfaceOre(r, x, y, z, 0, 3)
	}
}

// noSurfaceOre is NoSurfaceOreFeature (N5 (G)): n blocks, the i-th 2i random steps from the start,
// each set where it is netherrack within 6 of the start and touches no air.
func (d *decoration) noSurfaceOre(r *bmt, x, y, z, lo, hi int) {
	n := lo
	if lo < hi-1 {
		n += int(r.nextIntN(int32(hi - lo)))
	}
	for i := range n {
		px, py, pz := x, y, z
		if i > 0 {
			for range 2 * i {
				v := dirVec[r.nextIntN(6)]
				px, py, pz = px+v.x, py+v.y, pz+v.z
			}
		}
		dx, dy, dz := px-x, py-y, pz-z
		if dx*dx+dy*dy+dz*dz >= 37 || d.block(px, py, pz) != sNetherrack {
			continue
		}
		open := false
		for _, v := range dirVec {
			if d.block(px+v.x, py+v.y, pz+v.z).isAir() {
				open = true
				break
			}
		}
		if !open {
			d.set(px, py, pz, sAncientDebris)
		}
	}
}
