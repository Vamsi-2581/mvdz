// Independent MVDZ decoder written from FORMAT.md only (review exercise).
package main

import (
	"fmt"
	"hash/crc32"
	"math/bits"

	"github.com/klauspost/compress/zstd"
)

type decErr struct{ msg string }

func fail(f string, a ...any) { panic(decErr{fmt.Sprintf(f, a...)}) }

func catch(err *error) {
	if r := recover(); r != nil {
		if d, ok := r.(decErr); ok {
			*err = fmt.Errorf("%s", d.msg)
			return
		}
		panic(r)
	}
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func crc32c(b []byte) uint32 { return crc32.Checksum(b, castagnoli) }

// rd: byte reader with LEB128 (FORMAT.md §1).
type rd struct {
	b []byte
	p int
}

func (r *rd) left() int { return len(r.b) - r.p }

func (r *rd) u8() byte {
	if r.p >= len(r.b) {
		fail("read past end")
	}
	c := r.b[r.p]
	r.p++
	return c
}

func (r *rd) uv() uint64 {
	var v uint64
	for k := 0; k < 10; k++ {
		c := r.u8()
		if 7*k < 64 {
			v |= uint64(c&0x7f) << (7 * k)
		}
		if c < 0x80 {
			return v
		}
	}
	fail("uv: more than 10 bytes")
	return 0
}

func (r *rd) take(n uint64) []byte {
	if n > uint64(r.left()) {
		fail("take %d past end (%d left)", n, r.left())
	}
	s := r.b[r.p : r.p+int(n)]
	r.p += int(n)
	return s
}

func (r *rd) u16() uint16 { s := r.take(2); return uint16(s[0]) | uint16(s[1])<<8 }
func (r *rd) u32() uint32 { s := r.take(4); return le32(s) }
func (r *rd) u64() uint64 { s := r.take(8); return uint64(le32(s)) | uint64(le32(s[4:]))<<32 }

func le32(s []byte) uint32 {
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

var zdec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(1<<34))

// zstdFrames checks zstd data that may decode to at most max bytes
// against FORMAT.md §1: zstd or skippable frames only (no legacy magic),
// reserved bit 0, dictionary id 0, window <= max(max, 2^20), content size
// <= max, no reserved block type, no block over min(window, 128 KiB).
func zstdFrames(b []byte, max uint64) {
	if len(b) == 0 {
		fail("zstd: empty")
	}
	wmax := max
	if wmax < 1<<20 {
		wmax = 1 << 20
	}
	r := &rd{b: b}
	for r.left() > 0 {
		m := r.u32()
		if m&0xFFFFFFF0 == 0x184D2A50 {
			r.take(uint64(r.u32()))
			continue
		}
		if m != 0xFD2FB528 {
			fail("zstd: magic %08x", m)
		}
		d := r.u8()
		single := d&0x20 != 0
		if d&0x08 != 0 {
			fail("zstd: reserved bit")
		}
		var window uint64
		if !single {
			w := r.u8()
			base := uint64(1) << (10 + uint(w>>3))
			window = base + base/8*uint64(w&7)
		}
		for _, x := range r.take([]uint64{0, 1, 2, 4}[d&3]) {
			if x != 0 {
				fail("zstd: dictionary")
			}
		}
		fl := []uint64{0, 2, 4, 8}[d>>6]
		if d>>6 == 0 && single {
			fl = 1
		}
		var fcs uint64
		for i, x := range r.take(fl) {
			fcs |= uint64(x) << (8 * uint(i))
		}
		if fl == 2 {
			fcs += 256
		}
		if single {
			window = fcs
		}
		if window > wmax || (fl > 0 && fcs > max) {
			fail("zstd: window")
		}
		bmax := window
		if bmax > 1<<17 {
			bmax = 1 << 17
		}
		for {
			h := r.take(3)
			v := uint32(h[0]) | uint32(h[1])<<8 | uint32(h[2])<<16
			last, ty, size := v&1 != 0, (v>>1)&3, uint64(v>>3)
			if size > bmax || ty == 3 {
				fail("zstd: block")
			}
			if ty == 1 {
				r.take(1)
			} else {
				r.take(size)
			}
			if last {
				break
			}
		}
		if d&0x04 != 0 {
			r.take(4)
		}
	}
}

func unzstd(b []byte, max uint64) []byte {
	zstdFrames(b, max)
	out, err := zdec.DecodeAll(b, nil)
	if err != nil {
		fail("zstd: %v", err)
	}
	return out
}

func unzstdExact(b []byte, n uint64) []byte {
	out := unzstd(b, n)
	if uint64(len(out)) != n {
		fail("zstd size %d != %d", len(out), n)
	}
	return out
}

// q(v): sign/bit-length bucket.
func q(v int32) uint8 {
	if v == 0 {
		return 0
	}
	m := uint32(v)
	neg := uint8(0)
	if v < 0 {
		m = uint32(-int64(v))
		neg = 1
	}
	return uint8(2*(32-bits.LeadingZeros32(m))-1) + neg
}

func qb(v uint8) uint8 { return q(int32(int8(v))) }

func minU(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func clampI(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
