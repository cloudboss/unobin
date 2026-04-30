package lang

// ParseSource reads .ub source from b and returns the parsed File. The
// path is only used to populate Position.File on each AST node. Pass an
// empty string when parsing in-memory input.
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
	return v.(*File), nil
}
