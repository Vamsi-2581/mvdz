package main

// specdec: a conformance decoder for MVDZ, written from FORMAT.md alone
// (not from the Rust sources) to check that the specification is complete;
// tests/interop.rs runs it over the test vectors. It decodes whole files
// and favours plainness over speed. It is not the planned Go library.
// Usage: specdec file.mvdz [out]   prints "<sha256> <size>" or "ERROR: ..."

import (
	"crypto/sha256"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: specdec file.mvdz [out]")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	out, err := decodeV1(b)
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("%x %d\n", sha256.Sum256(out), len(out))
	if len(os.Args) > 2 {
		os.WriteFile(os.Args[2], out, 0o644)
	}
}
