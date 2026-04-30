package lang

// ParseSource reads .ub source from b and returns the parsed File. The
// path populates Position.File on each AST node and seeds Kind via
// ClassifyByFilename. Pass an empty string when parsing in-memory input.
// Callers loading config or exported-type files set Kind themselves.
//
// On parse failure, the returned error wraps pigeon's diagnostics. Callers
// that want structured errors should switch on the underlying type.
func ParseSource(path string, b []byte) (*File, error) {
	v, err := Parse(path, b,
		Entrypoint("File"),
		GlobalStore("file", path),
		Recover(false),
	)
	if err != nil {
		return nil, err
	}
	f := v.(*File)
	f.Kind = ClassifyByFilename(path)
	return f, nil
}
