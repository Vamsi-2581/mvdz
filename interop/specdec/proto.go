package main

// Protocol state and command sizer: FORMAT.md §6.1, §6.2.
const (
	fteFloat     = 0x8000
	fteTrans     = 0x8
	fteColourmod = 0x80000
	mvd1Float    = 1
)

type Proto struct {
	fte, fte2, mvd1 uint32
	floatCoords     bool
	msgWide         bool
	coord, wcoord   int
	angle           int
}

func newProto(fte, fte2, mvd1 uint32) Proto {
	p := Proto{fte: fte, fte2: fte2, mvd1: mvd1}
	p.derive()
	return p
}

func (p *Proto) derive() {
	p.floatCoords = p.fte&fteFloat != 0 || p.mvd1&mvd1Float != 0
	p.msgWide = p.fte&fteFloat != 0
	p.coord, p.wcoord, p.angle = 2, 2, 1
	if p.floatCoords {
		p.coord = 4
	}
	if p.msgWide {
		p.wcoord = 4
		p.angle = 2
	}
}

func (p *Proto) observeServerData(body []byte) {
	i := 0
	for i+4 <= len(body) {
		v := le32(body[i:])
		i += 4
		if v == 28 {
			break
		}
		if i+4 > len(body) {
			break
		}
		f := le32(body[i:])
		i += 4
		switch v {
		case 0x58455446:
			p.fte = f
		case 0x32455446:
			p.fte2 = f
		case 0x3144564D:
			p.mvd1 = f
		}
	}
	p.derive()
}

type sizer struct {
	b  []byte
	i  int
	ok bool
}

func (s *sizer) skip(n int) {
	if !s.ok || n < 0 || len(s.b)-s.i < n {
		s.ok = false
		return
	}
	s.i += n
}
func (s *sizer) u8() int {
	if !s.ok || s.i+1 > len(s.b) {
		s.ok = false
		return 0
	}
	s.i++
	return int(s.b[s.i-1])
}
func (s *sizer) u16() int {
	if !s.ok || s.i+2 > len(s.b) {
		s.ok = false
		return 0
	}
	s.i += 2
	return int(s.b[s.i-2]) | int(s.b[s.i-1])<<8
}
func (s *sizer) u32() uint32 {
	if !s.ok || s.i+4 > len(s.b) {
		s.ok = false
		return 0
	}
	s.i += 4
	return le32(s.b[s.i-4:])
}

// str: scan to and including NUL; returns length incl NUL
func (s *sizer) str() int {
	if !s.ok {
		return 0
	}
	for j := s.i; j < len(s.b); j++ {
		if s.b[j] == 0 {
			n := j + 1 - s.i
			s.i = j + 1
			return n
		}
	}
	s.ok = false
	return 0
}

const (
	uAngle1     = 1 << 0
	uAngle3     = 1 << 1
	uModel      = 1 << 2
	uColormap   = 1 << 3
	uSkin       = 1 << 4
	uEffects    = 1 << 5
	uEvenmore   = 1 << 7
	uOrigin1    = 1 << 9
	uOrigin2    = 1 << 10
	uOrigin3    = 1 << 11
	uAngle2     = 1 << 12
	uFrame      = 1 << 13
	uMorebits   = 1 << 15
	xScale      = 1 << 0
	xTrans      = 1 << 1
	xFatness    = 1 << 2
	xModeldbl   = 1 << 3
	xEntitydbl  = 1 << 5
	xEntitydbl2 = 1 << 6
	xYetmore    = 1 << 7
	xColourmod  = 1 << 10
)

func (p *Proto) delta(s *sizer, w int) {
	bits := w &^ 511
	more := 0
	if bits&uMorebits != 0 {
		bits |= s.u8()
	}
	if bits&uEvenmore != 0 && p.fte != 0 {
		more = s.u8()
		if more&xYetmore != 0 {
			more |= s.u8() << 8
		}
	}
	if bits&uModel != 0 {
		s.skip(1)
	} else if more&xModeldbl != 0 {
		s.skip(2)
	}
	for _, m := range []int{uFrame, uColormap, uSkin, uEffects} {
		if bits&m != 0 {
			s.skip(1)
		}
	}
	for _, oa := range [][2]int{{uOrigin1, uAngle1}, {uOrigin2, uAngle2}, {uOrigin3, uAngle3}} {
		if bits&oa[0] != 0 {
			s.skip(p.coord)
		}
		if bits&oa[1] != 0 {
			s.skip(p.angle)
		}
	}
	if more&xTrans != 0 && p.fte&fteTrans != 0 {
		s.skip(1)
	}
	if more&xScale != 0 {
		s.skip(1)
	}
	if more&xFatness != 0 {
		s.skip(1)
	}
	if more&xColourmod != 0 && p.fte&fteColourmod != 0 {
		s.skip(3)
	}
}

// cmdSize returns -1 for None.
func (p *Proto) cmdSize(cmd int, b []byte) int {
	s := &sizer{b: b, ok: true}
	c, w, a := p.coord, p.wcoord, p.angle
	switch cmd {
	case 1, 27, 28, 34, 35:
	case 2, 9, 26, 31:
		s.str()
	case 3:
		s.skip(2)
	case 6:
		ch := s.u16()
		if ch&0x8000 != 0 {
			s.skip(1)
		}
		if ch&0x4000 != 0 {
			s.skip(1)
		}
		s.skip(1 + 3*c)
	case 8:
		s.skip(1)
		s.str()
	case 10:
		s.skip(1 + 3*a)
	case 11:
		for s.ok {
			v := s.u32()
			if !s.ok || v == 28 {
				break
			}
			s.skip(4)
		}
		s.skip(4)
		s.str()
		s.skip(4)
		s.str()
		s.skip(40)
	case 12:
		s.skip(1)
		s.str()
	case 14:
		s.skip(3)
	case 19:
		s.skip(2 + 3*c)
	case 20:
		s.skip(4 + 3*(c+a))
	case 21, 66:
		wd := s.u16()
		if s.ok {
			p.delta(s, wd)
		}
	case 22:
		s.skip(2 + 4 + 3*(c+a))
	case 23:
		t := s.u8()
		switch t {
		case 5, 6, 9:
			s.skip(2 + 6*c)
		case 0, 1, 3, 4, 7, 8, 10, 11, 13:
			s.skip(3 * c)
		case 2, 12:
			s.skip(1 + 3*c)
		default:
			return -1
		}
	case 24, 32, 44:
		s.skip(1)
	case 29:
		s.skip(3*c + 3)
	case 30:
		s.skip(3*w + 3*a)
	case 36:
		s.skip(3)
	case 37, 38:
		s.skip(5)
	case 39, 53:
		s.skip(2)
	case 40:
		s.skip(5)
		s.str()
	case 41:
		sz := int(int16(uint16(s.u16())))
		s.skip(1)
		if sz > 0 {
			s.skip(sz)
		}
	case 42:
		s.skip(1)
		f := s.u16()
		s.skip(1)
		for i := 0; i < 3; i++ {
			if f&(1<<i) != 0 {
				s.skip(c)
			}
		}
		for i := 0; i < 3; i++ {
			if f&(8<<i) != 0 {
				s.skip(2)
			}
		}
		for _, m := range []int{1 << 11, 1 << 7, 1 << 6, 1 << 10} {
			if f&m != 0 {
				s.skip(1)
			}
		}
	case 43:
		n := s.u8()
		s.skip(6 * n)
	case 54:
		n := s.u8()
		s.skip(7 * n)
	case 45, 46:
		s.skip(1)
		for s.ok {
			if s.str() == 1 {
				break
			}
		}
		s.skip(1)
	case 47, 48:
		if cmd == 48 {
			s.skip(1)
		}
		for s.ok {
			wd := s.u16()
			if !s.ok || wd == 0 {
				break
			}
			p.delta(s, wd)
		}
	case 49, 50:
		s.skip(4)
	case 51:
		s.skip(1)
		s.str()
		s.str()
	case 52:
		s.str()
		s.str()
	default:
		return -1
	}
	if !s.ok {
		return -1
	}
	return s.i
}
