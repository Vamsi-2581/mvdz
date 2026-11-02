#![no_main]
//! Whole-file decoding of arbitrary input (1 and 2 threads), under a
//! small memory budget. A file that decodes must decode to its identity.
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    let l: mvdz::ReadOptions = mvdz::Limits {
        memory: 256 << 20,
        ..Default::default()
    }
    .into();
    let a = mvdz::decompress_with(data, 1, &l);
    let b = mvdz::decompress_with(data, 2, &l);
    assert_eq!(a.is_ok(), b.is_ok());
    if let (Ok(a), Ok(b)) = (a, b) {
        assert!(a == b);
    }
});
