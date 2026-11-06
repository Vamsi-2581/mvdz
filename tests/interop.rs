//! Cross-implementation test: interop/specdec, an independent decoder in Go
//! written from FORMAT.md, must accept and refuse exactly the vectors the
//! reference does, with the same decoded bytes, and decode files of both
//! levels made from sample demos. Skips when Go is not installed.

mod common;
use common::*;
use std::process::Command;

fn specdec() -> Option<std::path::PathBuf> {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let out = std::env::temp_dir().join(format!(
        "mvdz-specdec-{}{}",
        std::process::id(),
        std::env::consts::EXE_SUFFIX
    ));
    let ok = Command::new("go")
        .args(["build", "-o"])
        .arg(&out)
        .current_dir(root.join("interop/specdec"))
        .status()
        .ok()?
        .success();
    ok.then_some(out)
}

fn run(bin: &std::path::Path, file: &std::path::Path) -> Option<String> {
    let o = Command::new(bin).arg(file).output().unwrap();
    o.status.success().then(|| {
        String::from_utf8_lossy(&o.stdout)
            .split_whitespace()
            .next()
            .unwrap_or("")
            .to_string()
    })
}

#[test]
fn go_decoder_agrees() {
    let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("interop/specdec");
    if !dir.join("go.mod").exists() {
        // the crates.io package leaves interop/ out
        eprintln!("interop/specdec is not in this source tree (a packaged crate?), skipping");
        return;
    }
    let Some(bin) = specdec() else {
        eprintln!("go is not installed or the decoder does not build, skipping");
        return;
    };
    let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/vectors");
    let manifest = std::fs::read_to_string(dir.join("MANIFEST.tsv")).unwrap();
    for l in manifest.lines().filter(|l| !l.starts_with('#')) {
        let f: Vec<&str> = l.split('\t').collect();
        let got = run(&bin, &dir.join(f[0]));
        if f[1].starts_with("error:") {
            assert!(
                got.is_none(),
                "{}: the Go decoder accepts a hostile vector",
                f[0]
            );
        } else {
            assert_eq!(got.as_deref(), Some(f[1]), "{}", f[0]);
        }
    }
    let tmp = std::env::temp_dir().join(format!("mvdz-interop-{}.mvdz", std::process::id()));
    for src in samples(2) {
        for level in [mvdz::Level::Normal, mvdz::Level::Fast] {
            for c in [
                mvdz::compress(&src, &cfg(level, 0)).unwrap(),
                mvdz::compress(&src, &cfg(level, 300_000)).unwrap(),
                stream(&src, &cfg(level, 300_000)),
            ] {
                std::fs::write(&tmp, &c).unwrap();
                let sha: String = <sha2::Sha256 as sha2::Digest>::digest(&src)
                    .iter()
                    .map(|x| format!("{x:02x}"))
                    .collect();
                assert_eq!(run(&bin, &tmp), Some(sha));
            }
        }
    }
    let _ = std::fs::remove_file(&tmp);
    let _ = std::fs::remove_file(&bin);
}
