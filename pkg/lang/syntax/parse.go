package syntax

import "github.com/cloudboss/unobin/pkg/lang"

// LowerParsedSource lowers an already parsed UB source file into typed syntax.
func LowerParsedSource(path string, src []byte, f *lang.File) (*File, error) {
	out, errs := lowerFile(f, lowerMode{path: path, source: src})
	return out, errs.Err()
}

func ParseSource(path string, b []byte) (*File, error) {
	f, err := lang.ParseSource(path, b)
	if err != nil {
		return nil, err
	}
	return LowerParsedSource(path, b, f)
}
