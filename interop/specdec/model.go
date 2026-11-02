package main

import "math/bits"

// Range coder: FORMAT.md §8.3, "Range coder".
type rcDec struct {
	decisions uint64
	code, rng uint32
	buf       []byte
	pos       int
}

func newRC(b []byte) *rcDec {
	r := &rcDec{rng: 0xFFFFFFFF, buf: b}
	for i := 0; i < 5; i++ {
		r.code = r.code<<8 | uint32(r.next())
	}
	return r
}

func (r *rcDec) next() byte {
	var c byte
	if r.pos < len(r.buf) {
		c = r.buf[r.pos]
	}
	r.pos++
	return c
}

func (r *rcDec) bit(p uint32) int {
	r.decisions++
	bound := (r.rng >> 12) * p
	var b int
	if r.code < bound {
		b = 1
		r.rng = bound
	} else {
		r.code -= bound
		r.rng -= bound
	}
	for r.rng < 1<<24 {
		r.rng <<= 8
		r.code = r.code<<8 | uint32(r.next())
	}
	return b
}

func (r *rcDec) overrun() bool { return r.pos > len(r.buf)+5 }

// IntModel: FORMAT.md §8.3, "Adaptive probability P and IntModel".
type intModel struct{ P []uint16 }

func newIntModel(nctx int) *intModel {
	m := &intModel{P: make([]uint16, nctx*64)}
	for i := range m.P {
		m.P[i] = 32768
	}
	return m
}

func p12(p uint16) uint32 {
	v := uint32(p >> 4)
	if v < 1 {
		v = 1
	}
	if v > 4095 {
		v = 4095
	}
	return v
}

func (m *intModel) bitAt(rc *rcDec, k int, sh uint) int {
	b := rc.bit(p12(m.P[k]))
	if b == 1 {
		m.P[k] += (65535 - m.P[k]) >> sh
	} else {
		m.P[k] -= m.P[k] >> sh
	}
	return b
}

func (m *intModel) get(rc *rcDec, ctx int) uint32 {
	base := ctx * 64
	n := 0
	for n < 32 {
		if m.bitAt(rc, base+n, 4) == 0 {
			break
		}
		n++
	}
	v1 := uint64(1)
	for i := n - 1; i >= 0; i-- {
		k := base + 33 + min(i, 30)
		v1 = v1<<1 | uint64(m.bitAt(rc, k, 5))
	}
	return uint32(v1 - 1)
}

const nqc = 306

var pow4 = [4]uint64{65536, 77936, 92682, 110218}

func qval(qq uint32, steps uint32) uint64 {
	if qq == 0 {
		return 0
	}
	e := qq - 1
	o := e / steps
	k := e % steps
	frac := pow4[(k*4)/steps]
	if o >= 40 {
		return (^uint64(0)) >> 8
	}
	return frac << o
}

func normalize(w []uint64, T uint64) []uint32 {
	f := make([]uint64, len(w))
	var tot uint64 // fits: <= 256 * 2^56
	for _, x := range w {
		tot += x
	}
	if tot == 0 {
		return make([]uint32, len(w))
	}
	var sum int64
	for i, x := range w {
		if x > 0 {
			hi, lo := bits.Mul64(x, T)
			qq, _ := bits.Div64(hi, lo, tot)
			if qq < 1 {
				qq = 1
			}
			f[i] = qq
			sum += int64(qq)
		}
	}
	diff := int64(T) - sum
	for diff != 0 {
		bi := 0
		for i := range f {
			if f[i] > f[bi] {
				bi = i
			}
		}
		if diff > 0 {
			f[bi] += uint64(diff)
			diff = 0
		} else {
			take := min(-diff, int64(f[bi])-1, (int64(f[bi])+1)/2)
			if take <= 0 {
				break
			}
			f[bi] -= uint64(take)
			diff += take
		}
	}
	o := make([]uint32, len(w))
	for i := range f {
		o[i] = uint32(f[i])
	}
	return o
}

type table struct {
	freq, cum []uint32
	kind      int // 0 empty, 1 single, 2 normal
	sym       int
}

func mkTable(qs []uint32, steps uint32, T uint64) *table {
	w := make([]uint64, len(qs))
	for i, x := range qs {
		w[i] = qval(x, steps)
	}
	t := &table{freq: normalize(w, T)}
	t.cum = make([]uint32, len(qs)+1)
	nz := 0
	for i, fr := range t.freq {
		t.cum[i+1] = t.cum[i] + fr
		if fr > 0 {
			nz++
			t.sym = i
		}
	}
	switch nz {
	case 0:
		t.kind = 0
	case 1:
		t.kind = 1
	default:
		t.kind = 2
	}
	return t
}

func (t *table) find(slot uint32) int {
	// linear is fine for a reference decoder
	lo, hi := 0, len(t.freq)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.cum[mid] <= slot {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

type ransModel struct {
	alpha, steps, scale uint32
	fb                  *table
	keys                map[uint32]*table
}

func (m *ransModel) lookup(ctx uint32) *table {
	if t, ok := m.keys[ctx]; ok {
		return t
	}
	return m.fb
}

func readTable(im *intModel, rc *rcDec, alpha uint32, fb []uint32) []uint32 {
	t := make([]uint32, alpha)
	prev := uint32(0)
	for s := uint32(0); s < alpha; s++ {
		f := uint32(17)
		if fb != nil {
			f = minU(fb[s], 16)
		}
		t[s] = minU(im.get(rc, int(f*17+minU(prev, 16))), 160)
		prev = t[s]
	}
	return t
}

// LSB-first bit reader.
type bitRd struct {
	b      []byte
	k      uint64
	strict bool
}

func (r *bitRd) get(n uint32) uint32 {
	var v uint32
	for i := uint32(0); i < n; i++ {
		byt := r.k / 8
		var bit uint32
		if byt < uint64(len(r.b)) {
			bit = uint32(r.b[byt]>>(r.k%8)) & 1
		} else if r.strict {
			fail("mantissa bits exhausted")
		}
		v |= bit << i
		r.k++
	}
	return v
}

func bucketValue(b uint32, br *bitRd) int32 {
	if b > 64 {
		fail("bucket %d > 64", b)
	}
	n := (b + 1) / 2
	nb := uint32(0)
	if n > 0 {
		nb = n - 1
	}
	m := br.get(nb)
	if n > 0 {
		m |= 1 << nb
	}
	if b == 0 {
		return 0
	}
	if b%2 == 1 {
		return int32(m)
	}
	return -int32(m)
}

// readModelV1: FORMAT.md §8.3 with rules M1, M2 and the alphabet rule.
func readModelV1(hdr []byte, alpha uint32, count uint64) *ransModel {
	rc := newRC(hdr)
	im := newIntModel(nqc + 8)
	m := &ransModel{keys: map[uint32]*table{}, scale: 12}
	m.alpha = im.get(rc, nqc)
	m.steps = im.get(rc, nqc+1)
	n := im.get(rc, nqc+2)
	if m.alpha != alpha || m.steps < 1 || m.steps > 4 || n > 65535 || uint64(n) > count {
		fail("model")
	}
	maxDec := uint64(len(hdr))*128 + 1024
	fbq := readTable(im, rc, m.alpha, nil)
	m.fb = mkTable(fbq, m.steps, 4096)
	k := uint64(0)
	for i := uint32(0); i < n; i++ {
		if rc.decisions > maxDec {
			fail("model")
		}
		dk := im.get(rc, nqc+3)
		if i > 0 && dk == 0 {
			fail("model")
		}
		k += uint64(dk)
		if k > 0xFFFF {
			fail("model")
		}
		qs := readTable(im, rc, m.alpha, fbq)
		m.keys[uint32(k)] = mkTable(qs, m.steps, 4096)
		if rc.overrun() {
			fail("model")
		}
	}
	if rc.overrun() || rc.decisions > maxDec || rc.pos != len(hdr) {
		fail("model")
	}
	return m
}
