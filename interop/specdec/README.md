# specdec: MVDZ conformance decoder (Go)

A decoder for MVDZ version 1 written from [FORMAT.md](../../FORMAT.md)
alone, as a check that the specification is complete and unambiguous. It
reads a whole `.mvdz` file, applies every validation rule, and prints the
SHA-256 and size of the decoded demo (or `ERROR: ...`):

```
go build -o specdec . && ./specdec file.mvdz [out.mvd]
```

`tests/interop.rs` builds it and requires it to accept and refuse exactly
the vectors in `tests/vectors/` that the reference does, with the same
decoded bytes. It favours plainness over speed and has no API: it is not
the Go library planned for mvdz.
