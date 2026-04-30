package lang

// allowedTopLevelKeys is the set of identifier keys permitted at the
// top level of each file kind. A stack and an exported type body are
// identical. A module manifest is just description + exports. A config
// holds deployment identity, state config, input values, and module
// configurations.
var allowedTopLevelKeys = map[FileKind]map[string]struct{}{
	FileStack: {
		"description": {},
		"inputs":      {},
		"constraints": {},
		"imports":     {},
		"data":        {},
		"resources":   {},
		"actions":     {},
		"outputs":     {},
	},
	FileExportedType: {
		"description": {},
		"inputs":      {},
		"constraints": {},
		"imports":     {},
		"data":        {},
		"resources":   {},
		"actions":     {},
		"outputs":     {},
	},
	FileModule: {
		"description": {},
		"exports":     {},
	},
	FileConfig: {
		"stack":          {},
		"deployment-id":  {},
		"parallelism":    {},
		"state":          {},
		"inputs":         {},
		"configurations": {},
	},
}

// ValidateTopLevelKeys checks that every top-level field in f.Body uses
// an identifier key permitted for f.Kind, and that no key appears twice.
// Returns the collected errors. f.Kind must already be classified;
// FileUnknown produces a single error directing the caller to classify
// first.
func ValidateTopLevelKeys(f *File) *ErrorList {
	errs := NewErrorList(0)
	if f.Kind == FileUnknown {
		errs.Addf(ErrSchema, f.S.Start,
			"cannot validate top level keys: file kind is unknown (classify by filename or caller context first)")
		return errs
	}
	allowed, ok := allowedTopLevelKeys[f.Kind]
	if !ok {
		errs.Addf(ErrSchema, f.S.Start, "no top level key set defined for file kind %s", f.Kind)
		return errs
	}
	seen := make(map[string]Position, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == FieldString {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"top level key must be an identifier, got quoted string %q", fld.Key.String)
			continue
		}
		name := fld.Key.Name
		if fld.Key.IsMeta() {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"@-prefixed key %q is not allowed at top level", name)
			continue
		}
		if _, ok := allowed[name]; !ok {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"%q is not a valid top level key for a %s file", name, f.Kind)
			continue
		}
		if prev, dup := seen[name]; dup {
			errs.Addf(ErrSchema, fld.Key.S.Start,
				"duplicate top level key %q (first defined at %s)", name, prev)
			continue
		}
		seen[name] = fld.Key.S.Start
	}
	return errs
}
