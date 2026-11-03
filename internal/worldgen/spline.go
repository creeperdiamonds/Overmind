package worldgen

// The terrain splines, built as the game builds them (Mojang's algorithm, per the owner's decision).
// The knots and the rules that derive them are from the study's notes (NOTES-exact-seed.md section
// 7), as our own code. All arithmetic is single precision; every product is wrapped in float32 so
// that no platform fuses it into a multiply-add.

const (
	coordC = iota
	coordE
	coordW
	coordPV
)

// splineCoords holds the four inputs, already rounded to singles.
type splineCoords [4]float32

// spline is a cubic Hermite curve over one input. A knot's value can be another spline.
type spline struct {
	coord    int
	loc, der []float32
	val      []splineValue
}

type splineValue struct {
	c float32
	s *spline
}

func (v *splineValue) eval(in *splineCoords) float32 {
	if v.s != nil {
		return v.s.eval(in)
	}
	return v.c
}

func (s *spline) eval(in *splineCoords) float32 {
	v := in[s.coord]
	n := len(s.loc)
	i := 0
	for i < n && !(v < s.loc[i]) {
		i++
	}
	i--
	if i < 0 {
		return s.extend(v, 0, in)
	}
	if i == n-1 {
		return s.extend(v, n-1, in)
	}
	x1, x2 := s.loc[i], s.loc[i+1]
	span := x2 - x1
	t := (v - x1) / span
	y1, y2 := s.val[i].eval(in), s.val[i+1].eval(in)
	delta := y2 - y1
	a := float32(s.der[i]*span) - delta
	b := float32(-s.der[i+1]*span) + delta
	line := y1 + float32(t*delta)
	bump := a + float32(t*(b-a))
	return line + float32(float32(t*(1-t))*bump)
}

func (s *spline) extend(v float32, k int, in *splineCoords) float32 {
	y := s.val[k].eval(in)
	if s.der[k] == 0 {
		return y
	}
	return y + float32(s.der[k]*(v-s.loc[k]))
}

// splineBuilder appends knots to a spline.
type splineBuilder struct{ s *spline }

func onCoord(coord int) splineBuilder { return splineBuilder{&spline{coord: coord}} }

func (b splineBuilder) c(loc, value float32) splineBuilder { return b.cd(loc, value, 0) }

func (b splineBuilder) cd(loc, value, der float32) splineBuilder {
	b.s.loc, b.s.der = append(b.s.loc, loc), append(b.s.der, der)
	b.s.val = append(b.s.val, splineValue{c: value})
	return b
}

func (b splineBuilder) n(loc float32, child *spline) splineBuilder {
	b.s.loc, b.s.der = append(b.s.loc, loc), append(b.s.der, 0)
	b.s.val = append(b.s.val, splineValue{s: child})
	return b
}

func lerp32(t, a, b float32) float32 { return a + float32(t*(b-a)) }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func foldPV32(w float32) float32 {
	return -float32((abs32(abs32(w)-0.6666667) - 0.33333334) * 3.0)
}

func mountainContinentalness(r, mod float32) float32 {
	slope := 1 - float32((1-mod)*0.5)
	icpt := float32(0.5 * (1 - mod))
	v := float32(float32((r+1.17)*0.46082947)*slope) - icpt
	if r < -0.7 {
		return max(v, -0.2222)
	}
	return max(v, 0)
}

func mountainSpline(mod float32, saddle bool) *spline {
	slope := 1 - float32((1-mod)*0.5)
	icpt := float32(0.5 * (1 - mod))
	z := icpt/float32(0.46082947*slope) - 1.17
	lo, hi := mountainContinentalness(-1, mod), mountainContinentalness(1, mod)
	b := onCoord(coordPV)
	if -0.65 < z && z < 1 {
		v75, v65, zv := mountainContinentalness(-0.75, mod), mountainContinentalness(-0.65, mod), mountainContinentalness(z, mod)
		var quarter float32 = 0.25 // -0.75 - -1
		b.cd(-1, lo, (v75-lo)/quarter).c(-0.75, v75).c(-0.65, v65).c(z-0.01, zv)
		s := (hi - zv) / (1 - z)
		b.cd(z, zv, s).cd(1, hi, s)
		return b.s
	}
	s := (hi - lo) / 2
	if saddle {
		b.c(-1, max(0.2, lo)).cd(0, lerp32(0.5, lo, hi), s)
	} else {
		b.cd(-1, lo, s)
	}
	b.cd(1, hi, s)
	return b.s
}

func ridgeSpline(valley, low, mid, high, peaks, minSteep float32) *spline {
	d1 := max(float32(0.5*(low-valley)), minSteep)
	d2 := float32(5.0 * (mid - low))
	return onCoord(coordPV).
		cd(-1, valley, d1).
		cd(-0.4, low, min(d1, d2)).
		cd(0, mid, d2).
		cd(0.4, high, float32(2.0*(high-mid))).
		cd(1, peaks, float32(0.7*(peaks-high))).s
}

func erosionOffset(valley, hill, tall, mf, plain, swamp float32, extreme, saddle bool) *spline {
	m0 := mountainSpline(lerp32(mf, 0.6, 1.5), saddle)
	m1 := mountainSpline(lerp32(mf, 0.6, 1.0), saddle)
	m2 := mountainSpline(mf, saddle)
	half := float32(0.5 * mf)
	wide := ridgeSpline(valley-0.15, half, half, half, float32(0.6*mf), 0.5)
	narrow := ridgeSpline(valley, float32(plain*mf), float32(hill*mf), half, float32(0.6*mf), 0.5)
	plains := ridgeSpline(valley, plain, plain, hill, tall, 0.5)
	hills := onCoord(coordPV).c(-1, valley).n(-0.4, plains).c(0, tall+0.07).s
	swamps := ridgeSpline(-0.02, swamp, swamp, hill, tall, 0)
	b := onCoord(coordE).n(-0.85, m0).n(-0.7, m1).n(-0.4, m2).n(-0.35, wide).n(-0.1, narrow).n(0.2, plains)
	if extreme {
		b.n(0.4, plains).n(0.45, hills).n(0.55, hills).n(0.58, plains)
	}
	return b.n(0.7, swamps).s
}

func factorTable(base float32, shattered bool) *spline {
	bs := onCoord(coordW).c(-0.2, 6.3).c(0.2, base).s
	b := onCoord(coordE).n(-0.6, bs).
		n(-0.5, onCoord(coordW).c(-0.05, 6.3).c(0.05, 2.67).s).
		n(-0.35, bs).n(-0.25, bs).
		n(-0.1, onCoord(coordW).c(-0.05, 2.67).c(0.05, 6.3).s).
		n(0.03, bs)
	if shattered {
		ws := onCoord(coordW).c(0, base).c(0.1, 0.625).s
		rs := onCoord(coordPV).c(-0.9, base).n(-0.69, ws).s
		b.c(0.35, base).n(0.45, rs).n(0.55, rs).c(0.62, base)
	} else {
		pk := onCoord(coordPV).n(0.45, bs).c(0.7, 1.56).s
		xh := onCoord(coordPV).n(-0.7, bs).c(-0.15, 1.37).s
		b.n(0.05, pk).n(0.4, pk).n(0.45, xh).n(0.55, xh).c(0.58, base)
	}
	return b.s
}

func weirdJag(k float32) *spline {
	return onCoord(coordW).c(-0.01, float32(0.63*k)).c(0.01, float32(0.3*k)).s
}

func ridgeJag(peak, high float32) *spline {
	s, e := foldPV32(0.4), foldPV32(0.56666666)
	mid := (s + e) / 2.0
	b := onCoord(coordPV).c(s, 0)
	if high > 0 {
		b.n(mid, weirdJag(high))
	} else {
		b.c(mid, 0)
	}
	if peak > 0 {
		b.n(1, weirdJag(peak))
	} else {
		b.c(1, 0)
	}
	return b.s
}

func erosionJag(peak0, peak1, high0, high1 float32) *spline {
	r0, r1 := ridgeJag(peak0, high0), ridgeJag(peak1, high1)
	return onCoord(coordE).n(-1, r0).n(-0.78, r1).n(-0.5775, r1).c(-0.375, 0).s
}

var (
	offsetSpline = func() *spline {
		beach := erosionOffset(-0.15, 0, 0, 0.1, 0, -0.03, false, false)
		low := erosionOffset(-0.1, 0.03, 0.1, 0.1, 0.01, -0.03, false, false)
		mid := erosionOffset(-0.1, 0.03, 0.1, 0.7, 0.01, -0.03, true, true)
		high := erosionOffset(-0.05, 0.03, 0.1, 1.0, 0.01, 0.01, true, true)
		return onCoord(coordC).c(-1.1, 0.044).c(-1.02, -0.2222).c(-0.51, -0.2222).c(-0.44, -0.12).c(-0.18, -0.12).
			n(-0.16, beach).n(-0.15, beach).n(-0.1, low).n(0.25, mid).n(1.0, high).s
	}()
	factorSpline = onCoord(coordC).c(-0.19, 3.95).
			n(-0.15, factorTable(6.25, true)).n(-0.1, factorTable(5.47, true)).
			n(0.03, factorTable(5.08, true)).n(0.06, factorTable(4.69, false)).s
	jaggedSpline = onCoord(coordC).c(-0.11, 0).n(0.03, erosionJag(1.0, 0.5, 0, 0)).n(0.65, erosionJag(1, 1, 1, 0)).s
)
