package syntax

import "github.com/cloudboss/unobin/pkg/lang"

func ParseSource(path string, b []byte) (*File, error) {
	f, err := lang.ParseSource(path, b)
	if err != nil {
		return nil, err
	}
	out, errs := LowerFile(f)
	return out, errs.Err()
}
