//! zstd frames: libzstd (the `zstd` crate) natively, the pure-Rust
//! `ruzstd` decoder on wasm or with the `pure-zstd` feature.
//!
//! The format allows, wherever it stores zstd data, one or more zstd
//! frames (skippable frames are skipped) that decompress to exactly the
//! declared length; no dictionary; a content checksum, when present, must
//! match; every frame's window at most `max(N, 1 MiB)`. An empty body is
//! not a frame and is an error. [`check_frames`] applies the frame-level
//! rules before either decoder runs, so libzstd and ruzstd accept and
//! refuse the same bodies (legacy v0.1-v0.7 frames are refused by both),
//! and ruzstd never buffers more than that window.

use crate::budget::with_capacity;
use crate::error::{Error, Result};

/// Compresses `b` at `level` into one frame. Never empty.
pub fn compress(b: &[u8], level: i32) -> Vec<u8> {
    #[cfg(not(target_family = "wasm"))]
    {
        // fails only on allocation failure inside libzstd
        zstd::bulk::compress(b, level).expect("zstd compression")
    }
    #[cfg(target_family = "wasm")]
    {
        let _ = level;
        ruzstd::encoding::compress_to_vec(b, ruzstd::encoding::CompressionLevel::Fastest)
    }
}

/// Decompresses `b` to exactly `n` bytes. The caller has charged `n`
/// bytes to its budget.
pub fn decompress(b: &[u8], n: usize) -> Result<Vec<u8>> {
    let out = decompress_upto(b, n)?;
    if out.len() != n {
        return Err(Error::Corrupt("zstd data"));
    }
    Ok(out)
}

/// Largest window a frame of zstd data of N bytes may declare: max(N,
/// 2^20) (FORMAT.md §1).
pub const WINDOW_SLACK: u64 = 1 << 20;

/// Checks the frame structure of zstd data that may decode to at most
/// `max` bytes (FORMAT.md §1): one or more zstd frames (RFC 8878 §3.1.1)
/// or skippable frames, nothing else; no dictionary id; the reserved bit
/// clear; no reserved block type; a window (or single-segment content
/// size) of at most `max(max, 2^20)`; a content size, when present, of at
/// most `max`. Block contents are not decoded.
pub fn check_frames(b: &[u8], max: usize) -> Result<()> {
    let bad = Error::Corrupt("zstd data");
    if b.is_empty() {
        return Err(bad);
    }
    let wmax = (max as u64).max(WINDOW_SLACK);
    let mut i = 0usize;
    let take = |i: &mut usize, n: usize| -> Result<usize> {
        let at = *i;
        *i = at.checked_add(n).filter(|&e| e <= b.len()).ok_or(bad)?;
        Ok(at)
    };
    while i < b.len() {
        let m = u32::from_le_bytes(b.get(i..i + 4).ok_or(bad)?.try_into().unwrap());
        i += 4;
        if m & 0xFFFF_FFF0 == 0x184D_2A50 {
            let at = take(&mut i, 4)?;
            let n = u32::from_le_bytes(b[at..at + 4].try_into().unwrap()) as usize;
            take(&mut i, n)?;
            continue;
        }
        if m != 0xFD2F_B528 {
            return Err(bad);
        }
        let d = b[take(&mut i, 1)?];
        let (fcs_flag, single, reserved, checksum, did_flag) =
            (d >> 6, d & 0x20 != 0, d & 0x08 != 0, d & 0x04 != 0, d & 3);
        if reserved {
            return Err(bad);
        }
        let mut window = 0u64;
        if !single {
            let w = b[take(&mut i, 1)?];
            let base = 1u64 << (10 + (w >> 3));
            window = base + (base / 8) * (w & 7) as u64;
        }
        let dl = [0usize, 1, 2, 4][did_flag as usize];
        let at = take(&mut i, dl)?;
        if b[at..at + dl].iter().any(|&x| x != 0) {
            return Err(bad);
        }
        let fl = match fcs_flag {
            0 => single as usize,
            1 => 2,
            2 => 4,
            _ => 8,
        };
        let at = take(&mut i, fl)?;
        let mut fcs = None;
        if fl > 0 {
            let mut v = [0u8; 8];
            v[..fl].copy_from_slice(&b[at..at + fl]);
            fcs = Some(u64::from_le_bytes(v) + if fl == 2 { 256 } else { 0 });
        }
        if single {
            window = fcs.unwrap_or(0);
        }
        if window > wmax || fcs.is_some_and(|f| f > max as u64) {
            return Err(bad);
        }
        // RFC 8878 §3.1.1.2.4: no block exceeds min(window, 128 KiB)
        let block_max = window.min(1 << 17) as usize;
        loop {
            let at = take(&mut i, 3)?;
            let h = u32::from_le_bytes([b[at], b[at + 1], b[at + 2], 0]);
            let (last, ty, size) = (h & 1 != 0, (h >> 1) & 3, (h >> 3) as usize);
            if size > block_max {
                return Err(bad);
            }
            match ty {
                0 | 2 => take(&mut i, size)?,
                1 => take(&mut i, 1)?,
                _ => return Err(bad),
            };
            if last {
                break;
            }
        }
        if checksum {
            take(&mut i, 4)?;
        }
    }
    Ok(())
}

/// Decompresses `b` to at most `max` bytes (more is an error).
pub fn decompress_upto(b: &[u8], max: usize) -> Result<Vec<u8>> {
    check_frames(b, max)?;
    let mut out = with_capacity(max)?;
    #[cfg(not(any(target_family = "wasm", feature = "pure-zstd")))]
    {
        let mut d = zstd::bulk::Decompressor::new().map_err(|_| Error::Limit("out of memory"))?;
        // writes only into the reserved capacity; more output is an error
        d.decompress_to_buffer(b, &mut out)
            .map_err(|_| Error::Corrupt("zstd data"))?;
    }
    #[cfg(any(target_family = "wasm", feature = "pure-zstd"))]
    {
        pure::decompress(b, max, &mut out)?;
    }
    Ok(out)
}

#[cfg(any(target_family = "wasm", feature = "pure-zstd"))]
mod pure {
    use crate::error::{Error, Result};
    use ruzstd::decoding::errors::{FrameDecoderError, ReadFrameHeaderError};
    use ruzstd::decoding::{BlockDecodingStrategy, FrameDecoder};

    pub fn decompress(b: &[u8], n: usize, out: &mut Vec<u8>) -> Result<()> {
        let bad = Error::Corrupt("zstd data");
        let mut src: &[u8] = b;
        let mut fd = FrameDecoder::new();
        while !src.is_empty() {
            match fd.reset(&mut src) {
                Ok(()) => {}
                Err(FrameDecoderError::ReadFrameHeaderError(ReadFrameHeaderError::SkipFrame {
                    length,
                    ..
                })) => {
                    let l = length as usize;
                    if src.len() < l {
                        return Err(bad);
                    }
                    src = &src[l..];
                    continue;
                }
                Err(_) => return Err(bad),
            }
            let start = out.len();
            loop {
                let done = fd
                    .decode_blocks(&mut src, BlockDecodingStrategy::UptoBytes(1 << 20))
                    .map_err(|_| bad)?;
                if fd.can_collect() > n - out.len() {
                    return Err(bad);
                }
                if let Some(v) = fd.collect() {
                    out.extend_from_slice(&v);
                }
                if done {
                    break;
                }
            }
            if fd.can_collect() > n - out.len() {
                return Err(bad);
            }
            if let Some(v) = fd.collect() {
                out.extend_from_slice(&v);
            }
            let fcs = fd.content_size();
            if fcs != 0 && fcs != (out.len() - start) as u64 {
                return Err(bad);
            }
            if let Some(c) = fd.get_checksum_from_data() {
                if Some(c) != fd.get_calculated_checksum() {
                    return Err(bad);
                }
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn frames() {
        let a = b"hello hello hello hello".repeat(50);
        let z = super::compress(&a, 3);
        assert_eq!(super::decompress(&z, a.len()).unwrap(), a);
        assert!(super::decompress(&z, a.len() - 1).is_err());
        assert!(super::decompress(&z, a.len() + 1).is_err());
        assert!(super::decompress(&[], 0).is_err());
        // two frames and a skippable frame in between
        let mut two = z.clone();
        two.extend_from_slice(&[0x50, 0x2a, 0x4d, 0x18, 3, 0, 0, 0, 1, 2, 3]);
        two.extend_from_slice(&z);
        let mut aa = a.clone();
        aa.extend_from_slice(&a);
        assert_eq!(super::decompress(&two, aa.len()).unwrap(), aa);
    }
}
