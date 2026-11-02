# Changelog

## 1.0.0-rc.1

First public release: a release candidate, for comments. The file format
(version 1) may still change before 1.0; files written by a release
candidate are not guaranteed to be readable by 1.0.

* The MVDZ file format, version 1 ([FORMAT.md](FORMAT.md)): a section
  container with metadata (identity SHA-256, server info, roster, census,
  wall-clock sources and pauses), a seek index, independently decodable
  segments, attachments and a trailer; the structural transform of MVD
  demos with static rANS (normal level) or zstd (fast level) per stream.
* Integrity: CRC-32C per segment and over the whole demo; the SHA-256
  identity verified by every full decode unless the caller trusts the
  input. A bundle hash over the identity and the attachments.
* Readers: whole-file, sequential (pipes, sockets, VFS callbacks) and
  random access with seeking by demo time; all apply the same validation
  rules, bound memory and work by the input, and take a caller budget.
* Writers: one-shot and streaming (record straight to `.mvdz`); every
  output is checked by decoding it.
* Attachments: add, replace, remove (crash-safe, concurrent updaters
  serialised on POSIX), list and extract without decoding, export a copy
  with a selection by name.
* Interfaces: the `mvdz` command-line tool, the Rust library, a C ABI
  (`include/mvdz.h`, static and shared library), WebAssembly builds.
* Test vectors, a conformance decoder written from the specification in Go
  (`interop/specdec`), fuzz targets.
