package lang

import (
	"path/filepath"

	ufs "github.com/cloudboss/unobin/pkg/fs"
)

// Canonicalize parses UB source and reformats it in canonical form with
// string wrapping enabled: the same output `unobin fmt --wrap-strings`
// produces. Tools that generate .ub or config content pass their draft
// bytes through it so written files match the formatter. The name labels
// parse errors only.
func Canonicalize(name string, src []byte) ([]byte, error) {
	file, err := ParseSource(name, src)
	if err != nil {
		return nil, err
	}
	return FormatWith(file, FormatOptions{WrapStrings: true})
}

// WriteCanonical canonicalizes src and atomically writes it to path. The
// path's basename labels parse errors. This is the single way generated
// .ub and config files reach disk, so a new writer formats by default
// instead of emitting ad-hoc bytes.
func WriteCanonical(path string, src []byte) error {
	out, err := Canonicalize(filepath.Base(path), src)
	if err != nil {
		return err
	}
	return ufs.WriteFileAtomic(path, out, 0o644)
}
