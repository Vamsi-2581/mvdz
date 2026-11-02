package main

import "fmt"

// ---- back ends: FORMAT.md §8 ----

type lazyDec interface {
	sym(ctx uint32) byte
	done() bool
}

type numDec interface {
	val(f [6]uint8, sid int) int32
	done() bool
}

type bytesLazy struct {
	b []byte
	p int
}

func (s *bytesLazy) sym(uint32) byte {
	if s.p >= len(s.b) {
		fail("lazy stream exhausted")
	}
	s.p++
	return s.b[s.p-1]
}
func (s *bytesLazy) done() bool { return s.p == len(s.b) }

type lebNum struct {
	b []byte
	p int
}

func (s *lebNum) val([6]uint8, int) int32 {
	var u uint32
	for k := 0; k < 5; k++ {
		if s.p >= len(s.b) {
			fail("numeric store exhausted")
		}
		c := s.b[s.p]
		s.p++
		u |= uint32(c&0x7f) << (7 * k)
		if c < 0x80 {
			return int32(u>>1) ^ -int32(u&1)
		}
	}
	fail("numeric leb > 5 bytes")
	return 0
}
func (s *lebNum) done() bool { return s.p == len(s.b) }

// rANS decoder core: FORMAT.md §8.3, "Symbol decode"
type ransCore struct {
	m     *ransModel
	X     [4]uint32
	j     int
	words []byte
	wp    int
	extra *bitRd
	n     uint64
	count uint64
}

func (r *ransCore) word() uint32 {
	var v uint32
	if len(r.words)-r.wp >= 2 {
		v = uint32(r.words[r.wp]) | uint32(r.words[r.wp+1])<<8
	}
	r.wp += 2
	return v
}

func (r *ransCore) dec(ctx uint32) uint32 {
	r.n++
	t := r.m.lookup(ctx)
	if t.kind == 0 {
		fail("empty table ctx=%#x", ctx)
	}
	if t.kind == 1 {
		return uint32(t.sym)
	}
	jj := r.j
	r.j = (r.j + 1) % 4
	x := r.X[jj]
	slot := x & 4095
	s := t.find(slot)
	x = t.freq[s]*(x>>12) + slot - t.cum[s]
	if x < 1<<16 {
		x = x<<16 | r.word()
	}
	r.X[jj] = x
	return uint32(s)
}

func (r *ransCore) sym(ctx uint32) byte { return byte(r.dec(ctx & 0xFFFF)) }
func (r *ransCore) val(f [6]uint8, sid int) int32 {
	b := r.dec(numCtx(sid, f))
	return bucketValue(b, r.extra)
}
func (r *ransCore) done() bool { return r.n == r.count }

type streamEntry struct {
	id, backend, count, plen uint64
	body                     []byte
}

func isLazy(id uint64) bool { return id < 34 }
func isNum(id uint64) bool  { return id >= 48 && id < 66 }
func isBlob(id uint64) bool { return (id >= 80 && id <= 88) || (id >= 128 && id < 384) }

var _ = fmt.Sprintf
