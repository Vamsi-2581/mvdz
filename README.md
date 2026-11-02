# mvdz

**mvdz** is a lossless compressor and file format for QuakeWorld MVD demos
(`.mvd`). An `.mvdz` file is about a fifth of the raw demo and a third of
the demo's gzip, decodes back to the exact original bytes (checked by
SHA-256), can be read sequentially from a pipe or seeked by demo time, and
carries the demo's metadata (identity hash, server info, roster, a census
of its contents, wall-clock time) so catalogues can read it without
decoding. Files can also carry attachments stored beside the demo.

It comes as a command-line tool, a Rust library, a C library (static or
shared, for engines such as ezQuake and FTE) and builds for WebAssembly.
The format is specified in [FORMAT.md](FORMAT.md), precisely enough to
write an independent decoder; [`interop/specdec`](interop/specdec) is one,
in Go, written from the specification as a conformance check, and
`tests/vectors/` holds valid and hostile test files.

**Status: 1.0.0-rc.1, a release candidate for comments.** The file format
may still change before 1.0: files written by a release candidate are not
guaranteed to be readable by 1.0, so keep the original `.mvd` files for
now. Later additions after 1.0 will use the forward-compatible extension
points of FORMAT.md. A Go library is planned. Media attachments (voice,
commentary, transcripts) are
[proposed for version 2, open for discussion](proposals/media-attachments.md).

How it works: the demo is walked command by command (`svc_*` layouts from
mvdsv and ezQuake). Every field goes to its own stream; numeric fields
(player and entity origins, view angles, sound and temp-entity
coordinates) are coded as residuals against predictors. Each stream is then
entropy-coded: static rANS with per-context tables at the **normal** level
(the default), or zstd at the **fast** level. Anything that does not parse
is stored raw, so any input round-trips.

## Install and build

**Linux binaries.** Each release on GitHub has static Linux binaries
(x86-64, any distribution), with the C library and header beside the
`mvdz` tool, and a `SHA256SUMS` file:

| download | for |
|---|---|
| `mvdz-<version>-x86_64-unknown-linux-musl-v3.tar.gz` | any x86-64 CPU from about 2015 on (AVX2); faster |
| `mvdz-<version>-x86_64-unknown-linux-musl.tar.gz` | any x86-64 CPU (if unsure) |

**From source**, on any platform (only Linux is tested): Rust 1.89 or newer
and a C compiler (for the bundled libzstd).

```
git clone https://github.com/galfthan/mvdz && cd mvdz
cargo build --release            # target/release/mvdz, libmvdz.a, libmvdz.so
cargo install --path .           # or: put mvdz on your PATH
```

The default build runs on any CPU of its architecture. Building for the
machine you run on, with `RUSTFLAGS="-C target-cpu=native" cargo build
--release`, is faster on x86-64 CPUs with AVX2 (up to about 15 % at the
normal level; [BENCHMARKS.md](BENCHMARKS.md)).

## Quickstart

```
mvdz compress game.mvd game.mvdz             # normal level, one segment
mvdz compress --fast -s 1M game.mvd game.mvdz  # faster decode, seekable segments
mvdz decompress game.mvdz game.mvd
mvdz info game.mvdz | jq .sha256             # metadata, without decoding
```

## Ratios and speeds

Sizes: on a 72 GB archive of gzip-compressed demos (about 112 GB raw),
measured on an independent stratified sample of 585 demos, with 95 %
intervals. Speeds: the default (portable) release build, one core of a
4-core x86-64 VM, on 40 random demos (140 MB raw), CLI one process per
file. Method, samples, per-era ratios, more speeds and builds with
`-C target-cpu=native` (faster) are in [BENCHMARKS.md](BENCHMARKS.md).

| format | size / raw | decode, CLI, 1 process | decode, library, 1 thread |
|---|---|---|---|
| mvdz normal | 0.211 [0.206-0.217] | 23 MB/s | 29 MB/s |
| mvdz fast | 0.242 [0.237-0.249] | 35 MB/s | 47 MB/s |
| gzip -6 | 0.64 | 88 MB/s | |
| xz -9 | 0.47 | 28 MB/s | |
| zstd -19 | 0.55 | 118 MB/s | |

MB = 10^6 bytes of raw demo. The whole archive would take about 23.7 GB
at the normal level and 27.2 GB at the fast level. Recent demos (2023-26)
compress to about 0.18: servers now record about 137 frames per second
against about 72 before, at about the same compressed bytes per second of
game. Every decode checks the CRC-32C of each segment and of the whole
demo, and by default the SHA-256 identity (15-18 % of the decode time at
the normal level, 26-30 % at the fast level); a cache that verified the
identity once can decode with `--trusted`, about 1.2 to 1.4 times faster.
Compression runs at about 6 MB/s (normal) and 2.2 MB/s (fast) per thread,
including a check that the output decodes to the input.

## Command line

```
mvdz compress   [--fast] [-s size] [-j threads] [--ext key=value]... in.mvd out.mvdz
mvdz decompress [-j threads] [--max-memory size] [--trusted] in.mvdz out.mvd
mvdz info       file.mvdz...                 metadata, index, attachments as JSON
mvdz census     [--tsv] files-or-dirs...     census of .mvdz / .mvd files
mvdz attach add file.mvdz name path [--mime type] [--meta key=value]...
mvdz attach ls  file.mvdz [name-prefix]
mvdz attach extract file.mvdz name out
mvdz attach rm  file.mvdz name...
mvdz attach export in.mvdz out.mvdz [--keep name]... [--keep-prefix p]... [--drop name]... [--drop-prefix p]...
mvdz bench      [--levels normal,fast] [-s size] [-r reps] [-j threads] [--trusted] files...
```

`-` is standard input or output (`mvdz decompress game.mvdz - | ...`).
Sizes take K, M, G suffixes (binary). `-s 1M` cuts the demo into
independently decodable segments: seek points and parallel decoding, at a
small cost in size; the default is one segment (a file holds at most 65536
segments). Compression decodes its own output and compares before writing
anything. `--trusted` skips the SHA-256 identity check (see above); use it
only for files whose identity you verified before. `attach add` stores the
file as given; `--mime` defaults to `application/octet-stream`; `--meta`
adds a key/value, whose key needs a prefix (`x-...` or `vendor.`):
unprefixed keys are reserved for future versions (FORMAT.md §10.1).
`attach export` writes a copy that keeps the attachments matching a
`--keep` name or `--keep-prefix` (all, if none is given) and no `--drop`
name or `--drop-prefix`, e.g. a demo download without large recordings;
the source is not changed, and the output's bundle hash goes to standard
error. `info` prints the JSON described in FORMAT.md §3.2, including
`bundle_sha256` (§10.2).

```
mvdz compress game.mvd game.mvdz
mvdz compress --fast -s 1M game.mvd game.mvdz
mvdz decompress game.mvdz game.mvd
mvdz info game.mvdz | jq .map
mvdz attach add game.mvdz analysis/result.json result.json --mime application/json --meta x-tool=mvda
mvdz attach export game.mvdz - --drop-prefix rec/ > download.mvdz
```

## Hashes and trust

* **Identity**: the SHA-256 of the original `.mvd` (the hub's
  `demo_sha256`), stored in the metadata. The same for every level,
  segment size and set of attachments. Reading the metadata does not
  verify it; a full decode does (by default).
* **Integrity**: CRC-32C per segment and over the whole demo, always
  checked, also when seeking. `--trusted` decodes with these alone.
* **Bundle**: `bundle_sha256`, a SHA-256 over the identity and the
  attachments' names, MIME types, SHA-256s and keys; the same however the
  file was compressed. A download ETag, and what a signature would sign.

Files from elsewhere are untrusted input: decoders bound memory and work
(`Limits`, `max_memory`), demos can carry console commands
(`svc_stufftext`) that players must filter, and attachments are untrusted
content of any type, not access-controlled: anyone holding a file can read
all of them. FORMAT.md §14 covers this.

**Media attachments** (voice, commentary, transcripts, their timing and a
privacy convention) are not part of version 1: they are
[proposed for version 2, open for discussion](proposals/media-attachments.md).

## Rust

Not on crates.io yet: depend on it by git
(`mvdz = { git = "https://github.com/galfthan/mvdz" }`) or by path.

```rust
use mvdz::{Attachment, Config, Level, Limits, ReadOptions};

// whole buffers
let packed = mvdz::compress(&demo, &Config::default())?;                   // normal level
let fast = mvdz::compress(&demo, &Config { level: Level::Fast, ..Default::default() })?;
let demo2 = mvdz::decompress(&packed, 4)?;                                  // 4 threads
// under a memory cap, or trusted (no SHA-256, CRC checks only)
let demo3 = mvdz::decompress_with(&packed, 1, &Limits { memory: 256 << 20, ..Default::default() }.into())?;
let demo4 = mvdz::decompress_with(&packed, 1, &ReadOptions { trusted: true, ..Default::default() })?;

// sequential decoding from any io::Read; random access from io::Read + io::Seek
let mut r = mvdz::Reader::open(std::fs::File::open("game.mvdz")?, 2, &ReadOptions::default())?;
r.seek_time(60_000)?;                     // resume at the segment holding demo time 60 s
std::io::copy(&mut r, &mut out)?;
mvdz::decompress_stream(std::io::stdin(), std::io::stdout(), 1, &ReadOptions::default())?;

// streaming compression of a demo that is still being recorded
let mut w = mvdz::Writer::new(file, &Config { segment: 1 << 20, ..Default::default() })?;
w.write_demo(&chunk)?;                    // segments are written as they fill
w.finish()?;                              // metadata, index, trailer (or w.abort())

// metadata without decoding (identity not verified); attachments
let info = mvdz::read_metadata(std::fs::File::open("game.mvdz")?)?;
println!("{} {}", info.to_json(), mvdz::api::hex(&info.bundle_hash()));
for a in mvdz::list_attachments(std::fs::File::open("game.mvdz")?, "analysis/")? { println!("{} {}", a.name, a.size); }
let result = mvdz::read_attachment(std::fs::File::open("game.mvdz")?, "analysis/result.json")?;
let a = Attachment { name: "analysis/result.json".into(), mime: "application/json".into(),
                     kv: vec![("x-tool".into(), "mvda".into())], data: json_bytes };
mvdz::update_attachments(std::path::Path::new("game.mvdz"), &["old"], &[a])?;
// a copy without some attachments, e.g. for a download
let sel = mvdz::Selection { drop_prefix: vec!["rec/".into()], ..Default::default() };
let etag = mvdz::export_attachments(std::fs::File::open("game.mvdz")?, std::fs::File::create("dl.mvdz")?, |e| sel.matches(e))?;
```

`ReadOptions` (for decoding) holds `limits` and `trusted`; `Limits` has
`memory` (bytes, default 1 GiB on 64-bit) and `work_per_byte`, and is all
that metadata and attachment reading take. Errors (`mvdz::Error`) are `Format` (not MVDZ or an
unsupported version), `Corrupt` (any rule of FORMAT.md), `Limit` (the
budget), `Invalid` (a bad argument, a configuration a reader would
refuse), `NotFound`, `Io` and `Internal`. Decoders never panic on corrupt
input; they return an error. Every size a file declares is bounded by the
bytes actually present (FORMAT.md §9), and `Limits` caps the memory one
decode may use.

## C

`cargo build --release` builds `target/release/libmvdz.a` and `libmvdz.so`;
the interface is [`include/mvdz.h`](include/mvdz.h). Link the static
library with `-lpthread -ldl -lm`, or the shared one with `-lmvdz` (and
`LD_LIBRARY_PATH` or an rpath). The release profile keeps debug info for
profiling; `strip` the libraries for distribution.

```c
#include "mvdz.h"

/* decode through the engine's VFS callbacks, seek by time */
int err;
mvdz_reader *r = mvdz_open_callbacks(vfs_read, vfs_seek, file, 2, 0, 0, &err);
if (!r) { printf("%s\n", mvdz_strerror(err)); return; }
mvdz_seek_time(r, 60000, NULL, NULL);
long long n;
while ((n = mvdz_read(r, buf, sizeof buf)) > 0) feed_demo(buf, n);
mvdz_close(r);

/* record straight to .mvdz; segment 0 = 1 MiB for the writer */
mvdz_writer *w = mvdz_writer_open(vfs_write, out, MVDZ_LEVEL_NORMAL, 0, 1, &err);
mvdz_writer_write(w, data, len);          /* as the demo is recorded */
mvdz_writer_finish(w);                     /* or mvdz_writer_abort(w) */
```

Handles are ids (a stale handle is an error, not a crash), errors are
sticky, buffers are freed with `mvdz_free`, and callbacks must return
normally (no `longjmp`) and must not call back into their own handle.
Attachments are listed in the JSON of `mvdz_info_json`, read with
`mvdz_attachment_read` (which needs both read and seek callbacks), and
filtered into a copy with `mvdz_attachments_export`.
`examples/c/example.c` shows every call; the test `tests/c_api.rs`
compiles and runs it.

## WebAssembly

The library builds for `wasm32-unknown-unknown` and `wasm32-wasip1`
(`cargo build --lib --target wasm32-unknown-unknown`). There, zstd is the
pure-Rust `ruzstd` (the `pure-zstd` feature selects it natively too) and
decoding is single-threaded; files are identical whichever zstd decoder
reads them. The format's resource rules bound memory by the input, and
`ReadOptions::limits` caps it further, which matters in a browser.

## Tests

`cargo test --release` runs the unit tests, the golden vectors (valid and
hostile files through every reader including the C interface, and through
the Go conformance decoder when Go is installed), round trips at both
levels, segmented and streamed, corrupt-input and hostile-file tests,
metadata and attachment tests, and the regressions of the pre-release
reviews. `cargo test --features pure-zstd` runs them with the pure-Rust
zstd decoder. Everything runs on synthetic demos; tests that can also use
real demos read them from the directory in `MVDZ_TEST_DATA` (subdirectory
`sample/` with demos named `<sha256>.mvd`, `gen/` with constructed demos,
`poc/` with hostile model headers) and say so when it is unset.
`MVDZ_WRITE_VECTORS=1` regenerates the vectors (it needs the sample
demos they were made from). Fuzz targets are in `fuzz/` (`cargo +nightly
fuzz run decode`; also `stream_reader`, `read_metadata`, `attachments`,
`stream_writer`, `capi`, `compress_rt` and the structure-aware `gen_rt`).
The feature `scalar` forces the portable code paths (as on wasm).

## Contributing

Issues and pull requests are welcome on GitHub. Please run `cargo fmt`,
`cargo clippy --all-targets` and `cargo test` (CI runs them on Linux, with
the pure-zstd tests, a wasm build, the C example, the Go decoder and the
minimum Rust version). Changes to the file format go through FORMAT.md first; larger
ideas fit `proposals/`. See [CHANGELOG.md](CHANGELOG.md) for releases.

## Licence and dependencies

MIT, see [LICENSE](LICENSE). Dependencies (all permissive): `zstd`, `zstd-safe`,
`zstd-sys` (MIT/Apache-2.0; bundled libzstd BSD-3-Clause), `ruzstd`
(MIT, wasm and the `pure-zstd` feature), `sha2` and RustCrypto
dependencies, `serde` and `serde_json` (MIT/Apache-2.0), `libc` (only the
optional `sampler` profiler of the command-line tool).
