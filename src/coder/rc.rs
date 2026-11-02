//! Binary range coder (LZMA-style: 32-bit range, byte-wise normalisation,
//! carry propagation through a cache byte) with 12-bit probabilities.
//! `p` is always P(bit = 1) in 1..=4095. Written from the published LZMA
//! range-coder description; no code copied.

pub const PBITS: u32 = 12;
const TOP: u32 = 1 << 24;

pub struct Enc {
    /// binary decisions coded so far
    pub decisions: u64,
    low: u64,
    range: u32,
    cache: u8,
    cache_size: u64,
    pub out: Vec<u8>,
}

impl Default for Enc {
    fn default() -> Self {
        Self::new()
    }
}

impl Enc {
    pub fn new() -> Enc {
        Enc {
            decisions: 0,
            low: 0,
            range: u32::MAX,
            cache: 0,
            cache_size: 1,
            out: Vec::new(),
        }
    }

    #[inline(always)]
    fn shift_low(&mut self) {
        if (self.low as u32) < 0xFF00_0000 || (self.low >> 32) != 0 {
            let carry = (self.low >> 32) as u8;
            let mut c = self.cache;
            loop {
                self.out.push(c.wrapping_add(carry));
                c = 0xFF;
                self.cache_size -= 1;
                if self.cache_size == 0 {
                    break;
                }
            }
            self.cache = (self.low >> 24) as u8;
        }
        self.cache_size += 1;
        self.low = (self.low & 0x00FF_FFFF) << 8;
    }

    #[inline(always)]
    pub fn bit(&mut self, bit: u32, p: u32) {
        self.decisions += 1;
        let bound = (self.range >> PBITS) * p;
        if bit != 0 {
            self.range = bound;
        } else {
            self.low += bound as u64;
            self.range -= bound;
        }
        while self.range < TOP {
            self.range <<= 8;
            self.shift_low();
        }
    }

    pub fn finish(mut self) -> Vec<u8> {
        for _ in 0..5 {
            self.shift_low();
        }
        self.out
    }
}

pub struct Dec<'a> {
    /// binary decisions decoded so far
    pub decisions: u64,
    code: u32,
    range: u32,
    b: &'a [u8],
    pos: usize,
}

impl<'a> Dec<'a> {
    pub fn new(b: &'a [u8]) -> Dec<'a> {
        let mut d = Dec {
            decisions: 0,
            code: 0,
            range: u32::MAX,
            b,
            pos: 0,
        };
        for _ in 0..5 {
            d.code = (d.code << 8) | d.next() as u32;
        }
        d
    }

    #[inline(always)]
    fn next(&mut self) -> u8 {
        let v = self.b.get(self.pos).copied().unwrap_or(0);
        self.pos += 1;
        v
    }

    /// Bytes read so far (including the zeros past the end).
    pub fn pos(&self) -> usize {
        self.pos
    }

    /// Bytes consumed beyond the input (a sign of corruption if large).
    pub fn overrun(&self) -> bool {
        self.pos > self.b.len() + 5
    }

    #[inline(always)]
    pub fn bit(&mut self, p: u32) -> u32 {
        self.decisions += 1;
        let bound = (self.range >> PBITS) * p;
        let b = (self.code < bound) as u32;
        let m = b.wrapping_neg();
        self.code -= bound & !m;
        self.range = (bound & m) | (self.range.wrapping_sub(bound) & !m);
        while self.range < TOP {
            self.range <<= 8;
            self.code = (self.code << 8) | self.next() as u32;
        }
        b
    }
}

/// Adaptive probability with a fixed shift (used for side information).
#[derive(Clone, Copy)]
pub struct P(pub u16);

impl Default for P {
    fn default() -> Self {
        P(1 << 15)
    }
}

impl P {
    #[inline(always)]
    pub fn p12(self) -> u32 {
        ((self.0 as u32) >> 4).clamp(1, 4095)
    }
    #[inline(always)]
    pub fn up(&mut self, bit: u32, sh: u32) {
        if bit != 0 {
            self.0 += (65535 - self.0) >> sh;
        } else {
            self.0 -= self.0 >> sh;
        }
    }
}

/// Small adaptive integer coder for headers: Elias-gamma style binarisation
/// with adaptive bits per (context, position).
pub struct IntModel {
    p: Vec<P>,
}

impl IntModel {
    pub fn new(nctx: usize) -> IntModel {
        IntModel {
            p: vec![P::default(); nctx * 64],
        }
    }
    pub fn put(&mut self, e: &mut Enc, ctx: usize, v: u32) {
        let v1 = v as u64 + 1;
        let n = 63 - v1.leading_zeros(); // bits below the leading one
        let base = ctx * 64;
        for i in 0..n {
            let p = &mut self.p[base + i as usize];
            e.bit(1, p.p12());
            p.up(1, 4);
        }
        if n < 32 {
            let p = &mut self.p[base + n as usize];
            e.bit(0, p.p12());
            p.up(0, 4);
        }
        for i in (0..n).rev() {
            let b = ((v1 >> i) & 1) as u32;
            let p = &mut self.p[base + 33 + (i as usize).min(30)];
            e.bit(b, p.p12());
            p.up(b, 5);
        }
    }
    pub fn get(&mut self, d: &mut Dec, ctx: usize) -> u32 {
        let base = ctx * 64;
        let mut n = 0;
        while n < 32 {
            let p = &mut self.p[base + n as usize];
            let b = d.bit(p.p12());
            p.up(b, 4);
            if b == 0 {
                break;
            }
            n += 1;
        }
        let mut v1: u64 = 1;
        for i in (0..n).rev() {
            let p = &mut self.p[base + 33 + (i as usize).min(30)];
            let b = d.bit(p.p12());
            p.up(b, 5);
            v1 = (v1 << 1) | b as u64;
        }
        (v1 - 1) as u32
    }
}
