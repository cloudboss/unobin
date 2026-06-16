package parse

// ParseSource reads .ub source from b and returns the parsed File. The
// path populates Position.File on each AST node. Pass an empty string
// when parsing in-memory input. File.Kind is left at its zero value;
// callers that need it use pkg/lang's wrapper which classifies by
// filename.
//
// On parse failure, the returned error wraps pigeon's diagnostics.
// Callers that want structured errors should switch on the underlying
// type.
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

// ParseExpr parses b as a single UB expression and returns its AST.
// path labels Position.File on each node. Trailing content past the
// expression is rejected: parsing happens through a synthetic
// single-field file so the grammar's EOF rule rejects leftovers.
// Position columns are reported one greater than the input column
// because of the wrapping prefix; callers showing source context to
// users may want to adjust.
func ParseExpr(path string, b []byte) (Expr, error) {
	wrapped := make([]byte, 0, len(b)+4)
	wrapped = append(wrapped, 'x', ':', ' ')
	wrapped = append(wrapped, b...)
	wrapped = append(wrapped, '\n')
	f, err := ParseSource(path, wrapped)
	if err != nil {
		return nil, err
	}
	if len(f.Body.Fields) != 1 {
		return nil, Errorf(ErrParse, Position{File: path},
			"expected a single expression, got %d", len(f.Body.Fields))
	}
	return f.Body.Fields[0].Value, nil
}

// ParseType parses b as a UB type expression and returns its AST.
func ParseType(path string, b []byte) (TypeExpr, error) {
	return ParseTypeAt(path, b, Position{File: path, Line: 1, Column: 1})
}

// ParseTypeAt parses b as a UB type expression whose first byte starts
// at base in the source file.
func ParseTypeAt(path string, b []byte, base Position) (TypeExpr, error) {
	if base.File == "" {
		base.File = path
	}
	v, err := Parse(path, b,
		Entrypoint("TypeFile"),
		GlobalStore("file", path),
		GlobalStore("base", base),
		Recover(false),
	)
	if err != nil {
		return nil, err
	}
	return v.(TypeExpr), nil
}
