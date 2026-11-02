//! The decoders must never panic (or loop forever) on corrupt input:
//! random byte flips, truncations, splices and pure garbage behind a valid
//! magic. Every case must return `Err` or bytes whose checksums matched.

mod common;
use common::*;
use mvdz::Level;
use std::panic::{AssertUnwindSafe, catch_unwind};

fn demo() -> Option<Vec<u8>> {
    let mut d = duel()?;
    d.truncate(200_000); // keep it quick; the cut goes to the raw tail
    Some(d)
}

fn no_panic(b: &[u8]) {
    for threads in [1, 4] {
        let r = catch_unwind(AssertUnwindSafe(|| {
            mvdz::decompress(b, threads).map(|v| v.len())
        }));
        assert!(
            r.is_ok(),
            "decoder panicked on a corrupt input of {} bytes ({threads} threads)",
            b.len()
        );
    }
}

#[test]
fn garbage_never_panics() {
    let mut rng = Rng(0x1234_5678_9abc_def1);
    for i in 0..3000 {
        let n = rng.below(400);
        let mut b: Vec<u8> = (0..n).map(|_| rng.next() as u8).collect();
        if i % 2 == 0 && b.len() >= 6 {
            b[..4].copy_from_slice(b"MVDZ");
            b[4] = 1;
        }
        no_panic(&b);
        readers_agree(&b, "garbage");
    }
}

#[test]
fn mutated_streams_never_panic() {
    let Some(src) = demo() else {
        eprintln!("no demo available, skipping");
        return;
    };
    let mut rng = Rng(0xdead_beef_cafe_f00d);
    for level in [Level::Normal, Level::Fast] {
        for seg in [0usize, 50_000] {
            let (c, info) = mvdz::compress_info(&src, &cfg(level, seg)).unwrap();
            assert_eq!(info.fallback_segments, 0);
            assert_eq!(mvdz::decompress(&c, 1).unwrap(), src);
            for _ in 0..200 {
                let mut b = c.clone();
                match rng.below(4) {
                    0 => {
                        for _ in 0..1 + rng.below(8) {
                            let i = rng.below(b.len());
                            b[i] ^= 1 << rng.below(8);
                        }
                    }
                    1 => b.truncate(rng.below(b.len())),
                    2 => {
                        let i = rng.below(b.len());
                        let j = (i + rng.below(64)).min(b.len());
                        for x in &mut b[i..j] {
                            *x = rng.next() as u8;
                        }
                    }
                    _ => {
                        let i = rng.below(b.len());
                        let j = (i + rng.below(256)).min(b.len());
                        let chunk = b[i..j].to_vec();
                        let k = rng.below(b.len());
                        b.splice(k..k, chunk);
                    }
                }
                no_panic(&b);
                if let Some(v) = readers_agree(&b, "mutation") {
                    assert!(v == src, "a corrupt file decoded to other bytes");
                }
            }
        }
    }
}
