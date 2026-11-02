package main

// Structural and blob stream ids: FORMAT.md §5.4.
const (
	sTIME = iota
	sTYPE
	sMASK
	sCMD
	sESCLEN
	sHIDTAG
	sHIDLEN
	sHIDTYPE
	sHIDDMG
	sPINUM
	sPIFLAGS
	sPIFRAME
	sPIMODEL
	sPISKIN
	sPIEFF
	sPIWFRAME
	sPEHDR
	sPEENT
	sPEBITS
	sPEMORE
	sPEMODEL
	sPEFRAME
	sPEMISC
	sPEA
	sSTIDX
	sSTVAL
	sSTLIDX
	sSNDCH
	sSNDMISC
	sSNDNUM
	sTETYPE
	sTEMISC
	sPRLEVEL
	sFRAGS
)

const (
	bRAW = 80 + iota
	bTAIL
	bSET
	bTEXT
	bPING
	bNAILS
	bHIDMISC
	bSNDF
	bTEF
)

const NONE = 0xFFFFFFFF

type blobCur struct {
	b []byte
	p int
}

type PiRec struct {
	off, t uint32
	flags  uint16
	num    uint8
	fl     uint8
}
type PeRec struct {
	off, t uint32
	num    uint16
	bits   uint16
	model  uint8
	ubits  uint8
}
type SndRec struct {
	off  uint32
	refs [3]uint32
	kind uint8
	num  uint8
}
type TeRec struct {
	off  uint32
	refs [3]uint32
	ty   uint8
}
type StlRec struct {
	off uint32
	key uint16
}

type plState struct {
	flags     uint16
	frame, wf uint8
}
type entState struct {
	a, ares            [3]uint8
	frame, model, bits uint8
	lastPkt            uint32
}

type Walker struct {
	out   []byte
	p     int
	sym   func(L int, ctx uint32) byte
	blobs map[int]*blobCur
	proto Proto

	t                                 uint32
	target, lastType, lastTb, prevCmd uint8
	firstByTy                         [8]uint8
	piPrevNum, stPrevIdx, stPrevVal   uint8
	lastHdr, prevTe, pePrevD          uint8
	pl                                [256]plState
	ent                               [2048]entState
	st                                [32][256]uint8
	plOff                             [256][3]uint32
	entOff                            [2048][3]uint32
	prevPkt, curPkt                   []uint16
	pktPtr                            int
	pktNo                             uint32

	pis  []PiRec
	pes  []PeRec
	snds []SndRec
	tes  []TeRec
	stls []StlRec

	share    bool // demoinfo blocks from the share (FORMAT.md §5.3)
	dinfo    *rd
	piRecs   [256][][]byte // P2 record bytes per player, consumed in order
	piFloat  [256][]bool
	piT      [256][]uint32
	piFlags  [256][]uint16
	piIdx    [256]int
	piOutOff [256][]uint32 // output offsets of each player's records (filled by P3)
}

func newWalker(out []byte, sym func(int, uint32) byte, blobs map[int]*blobCur, pr Proto) *Walker {
	w := &Walker{out: out, sym: sym, blobs: blobs, proto: pr, piPrevNum: 255, stPrevIdx: 255, pktNo: 1}
	for i := range w.plOff {
		w.plOff[i] = [3]uint32{NONE, NONE, NONE}
	}
	for i := range w.entOff {
		w.entOff[i] = [3]uint32{NONE, NONE, NONE}
	}
	return w
}

func (w *Walker) wr(b byte) {
	if w.p >= len(w.out) {
		fail("write past raw_len")
	}
	w.out[w.p] = b
	w.p++
}

func (w *Walker) b8(L int, ctx uint32) uint8 {
	v := w.sym(L, ctx)
	w.wr(v)
	return v
}

func (w *Walker) r8(L int, ctx uint32, pred uint8) uint8 {
	v := w.sym(L, ctx) + pred
	w.wr(v)
	return v
}

func (w *Walker) bn(L int, ctx uint32, n int) uint32 {
	var v uint32
	for k := 0; k < n; k++ {
		v |= uint32(w.b8(L, ctx|uint32(k)<<14)) << (8 * k)
	}
	return v
}

func (w *Walker) blob(id int, n int) {
	b := w.blobs[id]
	if b == nil {
		if n == 0 {
			return
		}
		fail("absent blob %d", id)
	}
	if n < 0 || len(b.b)-b.p < n {
		fail("blob %d short", id)
	}
	if len(w.out)-w.p < n {
		fail("blob write past raw_len")
	}
	copy(w.out[w.p:], b.b[b.p:b.p+n])
	b.p += n
	w.p += n
}

func (w *Walker) reserve(n int) uint32 {
	off := w.p
	w.p += n
	if w.p > len(w.out) {
		fail("reserve past raw_len")
	}
	return uint32(off)
}

func (w *Walker) generic(id int, c int) {
	b := w.blobs[id]
	var rest []byte
	if b != nil {
		rest = b.b[b.p:]
	}
	n := w.proto.cmdSize(c, rest)
	if n < 0 {
		fail("cmd_size(%d) None (blob %d)", c, id)
	}
	w.blob(id, n)
}

// ---- frames: FORMAT.md §6.4 ----
func (w *Walker) run(tail int) {
	stop := len(w.out) - tail
	if stop < 0 {
		stop = 0
	}
	for w.p < stop {
		w.frame()
	}
	if w.p != stop {
		fail("walk overran stop")
	}
	w.blob(bTAIL, tail)
}

func (w *Walker) frameHeader() (tb, ty uint8) {
	tb = w.b8(sTIME, uint32(w.lastType)|uint32(w.lastTb)<<8)
	w.lastTb = tb
	w.t += uint32(tb)
	ty = w.b8(sTYPE, uint32(w.lastType)|uint32(min(tb, 32))<<8)
	w.lastType = ty
	w.target = ty >> 3
	return
}

func (w *Walker) frame() {
	_, ty := w.frameHeader()
	switch ty & 7 {
	case 2:
		w.blob(bSET, 8)
	case 3:
		mask := w.bn(sMASK, 0, 4)
		w.lengthPrefixed(ty, mask == 0)
	case 1, 4, 5, 6:
		w.lengthPrefixed(ty, false)
	default:
		fail("frame type %d", ty&7)
	}
}

func (w *Walker) lengthPrefixed(ty uint8, hidden bool) {
	at := w.reserve(4)
	if hidden {
		w.hidden()
	} else {
		w.payload(ty)
	}
	n := uint32(w.p) - at - 4
	w.out[at] = byte(n)
	w.out[at+1] = byte(n >> 8)
	w.out[at+2] = byte(n >> 16)
	w.out[at+3] = byte(n >> 24)
}

func cmdClass(c uint8) uint8 {
	switch c {
	case 0:
		return 0
	case 42:
		return 1
	case 48:
		return 2
	case 47:
		return 3
	case 6:
		return 4
	case 23:
		return 5
	case 3:
		return 6
	case 38:
		return 7
	case 8:
		return 8
	case 51:
		return 9
	case 14:
		return 10
	case 36:
		return 11
	case 9:
		return 12
	case 43, 54:
		return 13
	case 19:
		return 14
	}
	return 15
}

func (w *Walker) payloadReset() {
	w.prevCmd = 0
	w.piPrevNum = 255
	w.stPrevIdx = 255
	w.stPrevVal = 0
}

func (w *Walker) payload(ty uint8) {
	w.payloadReset()
	idx := uint32(0)
	tyi := ty & 7
	firstPrev := cmdClass(w.firstByTy[tyi])
	first := uint8(255)
	for {
		ctx := uint32(cmdClass(w.prevCmd)) | min(idx, 15)<<4 | uint32(firstPrev)<<8 | uint32(tyi)<<12 | uint32(w.prevCmd)<<16
		idx++
		if idx == 2 {
			first = w.prevCmd
		}
		if idx >= 2 {
			w.firstByTy[tyi] = first
		} else {
			w.firstByTy[tyi] = 255
		}
		cmd := w.sym(sCMD, ctx)
		if cmd == 255 {
			return
		}
		if cmd == 0 {
			w.escape()
			return
		}
		w.wr(cmd)
		w.command(cmd)
		w.prevCmd = cmd
	}
}

func (w *Walker) escape() {
	n := uint32(0)
	for k := 0; k < 5; k++ {
		b := w.sym(sESCLEN, uint32(k))
		n |= uint32(b&0x7f) << (7 * k)
		if b < 0x80 {
			break
		}
		if k == 4 {
			fail("esclen too long")
		}
	}
	w.blob(bRAW, int(n))
}

func (w *Walker) command(cmd uint8) {
	switch cmd {
	case 42:
		w.playerinfo()
	case 47, 48:
		w.packetEntities(cmd)
	case 3:
		t := w.target & 31
		ictx := uint32(w.stPrevIdx) | uint32(w.prevCmd)<<16
		i := w.b8(sSTIDX, ictx)
		prev := w.st[t][i]
		v := w.r8(sSTVAL, uint32(i)|uint32(w.stPrevVal)<<8, prev)
		w.stPrevIdx = i
		w.stPrevVal = v - prev
		w.st[t][i] = v
	case 38:
		t := w.target & 31
		i := w.b8(sSTLIDX, uint32(w.prevCmd))
		off := w.reserve(4)
		w.stls = append(w.stls, StlRec{off, uint16(t)<<8 | uint16(i)})
	case 6:
		w.sound()
	case 23:
		w.tempEntity()
	case 8:
		w.b8(sPRLEVEL, 0)
		w.generic(bTEXT, 9)
	case 9, 26:
		w.generic(bTEXT, 9)
	case 14:
		w.bn(sFRAGS, 0, 3)
	case 36, 37, 53:
		w.generic(bPING, int(cmd))
	case 43, 54:
		w.generic(bNAILS, int(cmd))
	case 11:
		start := w.p
		w.generic(128+11, 11)
		w.proto.observeServerData(w.out[start:w.p])
	default:
		w.generic(128+int(cmd), int(cmd))
	}
}

func popc(x uint32) int {
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}

func (w *Walker) playerinfo() {
	num := w.b8(sPINUM, uint32(w.piPrevNum)|uint32(w.prevCmd)<<16)
	w.piPrevNum = num
	ps := w.pl[num]
	flags := w.piFlags_(ps.flags)
	frame := w.r8(sPIFRAME, uint32(ps.frame)|(uint32(flags)&0x300)<<8, ps.frame)
	cw := w.proto.coord
	off := w.reserve(popc(uint32(flags)&7)*cw + popc(uint32(flags)&0x38)*2)
	fl := uint8(0)
	if w.proto.floatCoords {
		fl = 1
	}
	w.pis = append(w.pis, PiRec{off, w.t, flags, num, fl})
	o := off
	for i := 0; i < 3; i++ {
		if flags&(1<<i) != 0 {
			w.plOff[num][i] = o
			o += uint32(cw)
		}
	}
	if flags&(1<<11) != 0 {
		w.b8(sPIMODEL, 0)
	}
	if flags&(1<<7) != 0 {
		w.b8(sPISKIN, 0)
	}
	if flags&(1<<6) != 0 {
		w.b8(sPIEFF, 0)
	}
	wf := ps.wf
	if flags&(1<<10) != 0 {
		wf = w.r8(sPIWFRAME, uint32(ps.wf), ps.wf)
	}
	w.pl[num] = plState{flags, frame, wf}
}

func (w *Walker) piFlags_(prev uint16) uint16 {
	pv := uint32(prev)
	cx := (pv & 0x3f) | ((pv>>10)&1)*64 | pv<<16
	a := uint32(w.sym(sPIFLAGS, cx))
	x := (a & 0x3f) | ((a>>6)&1)<<10
	if a&0x80 != 0 {
		b1 := uint32(w.sym(sPIFLAGS, 0x100|((pv>>6)&0xff)))
		b2 := uint32(w.sym(sPIFLAGS, 0x200))
		x |= (b1&0xf)<<6 | (b1>>4)<<11 | (b2&1)<<15
	}
	flags := uint16(x) ^ prev
	w.wr(byte(flags))
	w.wr(byte(flags >> 8))
	return flags
}

func (w *Walker) predictEnt(prev int32) int32 {
	for w.pktPtr < len(w.prevPkt) && int32(w.prevPkt[w.pktPtr]) <= prev {
		w.pktPtr++
	}
	if w.pktPtr < len(w.prevPkt) {
		return int32(w.prevPkt[w.pktPtr])
	}
	return prev + 1
}

func (w *Walker) packetEntities(cmd uint8) {
	if cmd == 48 {
		h := w.r8(sPEHDR, 0, w.lastHdr)
		w.lastHdr = h
	}
	w.pePrevD = 0
	prevEnt := int32(-1)
	w.pktPtr = 0
	w.curPkt = w.curPkt[:0]
	w.pktNo++
	defer func() { w.prevPkt, w.curPkt = w.curPkt, w.prevPkt }()
	for {
		pe := w.predictEnt(prevEnt)
		ectx := uint32(min(pe-prevEnt, 255)) | uint32(min(w.pePrevD, 15))<<8
		d := uint32(w.sym(sPEENT, ectx))
		if d == 255 {
			d = uint32(w.sym(sPEENT, 1<<12)) | uint32(w.sym(sPEENT, 2<<12))<<8
		}
		if d == 0 {
			w.wr(0)
			w.wr(0)
			break
		}
		ent := (prevEnt + int32(d)) & 511
		es := w.ent[ent]
		b := w.sym(sPEBITS, uint32(es.bits)|uint32(es.model)<<7) ^ es.bits
		wd := uint16(uint32(b)<<9 | uint32(ent))
		if wd == 0 {
			fail("entity word 0")
		}
		w.wr(byte(wd))
		w.wr(byte(wd >> 8))
		w.pePrevD = uint8(clampI(ent-prevEnt, 0, 255))
		prevEnt = ent
		w.curPkt = append(w.curPkt, uint16(ent))
		w.ent[ent].bits = uint8(wd >> 9)
		w.entityDelta(wd)
	}
}

func (w *Walker) entityDelta(wd uint16) {
	bits := uint32(wd) &^ 511
	more := uint32(0)
	num := uint32(wd) & 511
	m0 := uint32(w.ent[num].model)
	if bits&uMorebits != 0 {
		bits |= uint32(w.b8(sPEMORE, (bits>>9)|m0<<8))
	}
	if bits&uEvenmore != 0 && w.proto.fte != 0 {
		more = uint32(w.b8(sPEMORE, 0x8000))
		if more&xYetmore != 0 {
			more |= uint32(w.b8(sPEMORE, 0x8001)) << 8
		}
	}
	if more&xEntitydbl != 0 {
		num += 512
	}
	if more&xEntitydbl2 != 0 {
		num += 1024
	}
	inPrev := w.ent[num].lastPkt == w.pktNo-1
	w.ent[num].lastPkt = w.pktNo
	es := w.ent[num]
	if bits&uModel != 0 {
		es.model = w.b8(sPEMODEL, uint32(es.model))
	} else if more&xModeldbl != 0 {
		w.bn(sPEMODEL, 0x100, 2)
	}
	model := uint32(es.model)
	if bits&uFrame != 0 {
		es.frame = w.r8(sPEFRAME, model, es.frame)
	}
	if bits&uColormap != 0 {
		w.b8(sPEMISC, model)
	}
	if bits&uSkin != 0 {
		w.b8(sPEMISC, model|1<<8)
	}
	if bits&uEffects != 0 {
		w.b8(sPEMISC, model|2<<8)
	}
	org := [3]uint32{uOrigin1, uOrigin2, uOrigin3}
	ang := [3]uint32{uAngle1, uAngle2, uAngle3}
	rb := uint16(0)
	for i := 0; i < 3; i++ {
		if bits&org[i] != 0 {
			rb |= 1 << (2 * i)
		}
		if bits&ang[i] != 0 {
			rb |= 2 << (2 * i)
		}
	}
	if inPrev {
		rb |= 64
	}
	if w.proto.floatCoords {
		rb |= 128
	}
	if w.proto.msgWide {
		rb |= 256
	}
	rec := PeRec{uint32(w.p), w.t, uint16(num), rb, es.model, uint8(wd >> 9)}
	for i := 0; i < 3; i++ {
		if bits&org[i] != 0 {
			w.entOff[num][i] = w.reserve(w.proto.coord)
		}
		if bits&ang[i] != 0 {
			if w.proto.msgWide {
				w.bn(sPEA, 3|model<<6, 2)
			} else {
				qa := uint32(min(qb(es.ares[i]), 15))
				v := w.r8(sPEA, uint32(i)|qa<<2|model<<6, es.a[i])
				es.ares[i] = v - es.a[i]
				es.a[i] = v
			}
		}
	}
	w.pes = append(w.pes, rec)
	if more&xTrans != 0 && w.proto.fte&fteTrans != 0 {
		w.b8(sPEMISC, 0x3000)
	}
	if more&xScale != 0 {
		w.b8(sPEMISC, 0x3001)
	}
	if more&xFatness != 0 {
		w.b8(sPEMISC, 0x3002)
	}
	if more&xColourmod != 0 && w.proto.fte&fteColourmod != 0 {
		w.bn(sPEMISC, 0x3003, 3)
	}
	w.ent[num] = es
}

func (w *Walker) sound() {
	lo := uint32(w.b8(sSNDCH, uint32(w.prevCmd)))
	ch := lo | uint32(w.b8(sSNDCH, 0x100|lo))<<8
	if ch&0x8000 != 0 {
		w.b8(sSNDMISC, 0)
	}
	if ch&0x4000 != 0 {
		w.b8(sSNDMISC, 1)
	}
	ent := (ch >> 3) & 1023
	num := w.b8(sSNDNUM, (ch&7)|min(ent, 33)<<3)
	if w.proto.coord != 2 {
		w.blob(bSNDF, 12)
		return
	}
	var refs [3]uint32
	var kind uint8
	if ent >= 1 && ent <= 32 {
		refs = w.plOff[ent-1]
		kind = 1
	} else {
		refs = w.entOff[ent]
		kind = 2
	}
	off := w.reserve(6)
	w.snds = append(w.snds, SndRec{off, refs, kind, num})
}

func isBeam(t uint8) bool { return t == 5 || t == 6 || t == 9 }

func (w *Walker) tempEntity() {
	pc := uint32(0)
	if w.prevCmd == 23 {
		pc = 1
	}
	t := w.b8(sTETYPE, uint32(w.prevTe)|pc<<8)
	w.prevTe = t
	c := w.proto.coord
	none3 := [3]uint32{NONE, NONE, NONE}
	if c != 2 {
		if isBeam(t) {
			w.bn(sTEMISC, uint32(t), 2)
			w.blob(bTEF, 6*c)
		} else if t == 2 || t == 12 {
			w.b8(sTEMISC, uint32(t)|0x100)
			w.blob(bTEF, 3*c)
		} else {
			w.blob(bTEF, 3*c)
		}
		return
	}
	if isBeam(t) {
		e := w.bn(sTEMISC, uint32(t), 2)
		refs := none3
		if e >= 1 && e <= 32 {
			refs = w.plOff[e-1]
		}
		off := w.reserve(12)
		w.tes = append(w.tes, TeRec{off, refs, t})
	} else if t == 2 || t == 12 {
		w.b8(sTEMISC, uint32(t)|0x100)
		off := w.reserve(6)
		w.tes = append(w.tes, TeRec{off, none3, t})
	} else {
		off := w.reserve(6)
		w.tes = append(w.tes, TeRec{off, none3, t})
	}
}

func (w *Walker) hidden() {
	for {
		tag := w.sym(sHIDTAG, 0)
		if tag == 0 {
			w.escape()
			return
		}
		if tag == 2 {
			return
		}
		if tag != 1 {
			fail("hid.tag %d", tag)
		}
		bl := w.bn(sHIDLEN, 0, 4)
		ty := w.bn(sHIDTYPE, 0, 2)
		if ty == 7 && bl == 8 {
			for k := uint32(0); k < 4; k++ {
				w.bn(sHIDDMG, k, 2)
			}
		} else if w.share && ty == 3 {
			// FORMAT.md §5.3, §6.11: from the demoinfo share
			if w.dinfo == nil {
				fail("demoinfo share missing")
			}
			n := w.dinfo.u32()
			if n != bl {
				fail("demoinfo length %d != block length %d", n, bl)
			}
			body := w.dinfo.take(uint64(n))
			if len(w.out)-w.p < len(body) {
				fail("demoinfo past raw_len")
			}
			copy(w.out[w.p:], body)
			w.p += len(body)
		} else {
			w.blob(bHIDMISC, int(bl))
		}
	}
}
