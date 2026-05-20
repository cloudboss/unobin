package runtime

// ModuleSchema describes a Go module's registered resource, data
// source, and action types as the dev CLI sees them at compile time.
// Each entry is keyed by the type's kebab-case name (the same name
// used in stack source).
type ModuleSchema struct {
	Resources   map[string]*TypeSchema
	DataSources map[string]*TypeSchema
	Actions     map[string]*TypeSchema
}

// TypeSchema describes the input and output fields of one resource,
// data source, or action. Each map keys a kebab-case field name
// (the form stack source uses) to that field's Go type expression
// as written in source (e.g. `string`, `[]string`, `time.Duration`).
// Inputs lists the receiver type's exported fields; Outputs lists
// the output struct's. The type checker turns these strings into
// typecheck.Types at the comparison site.
type TypeSchema struct {
	Inputs  map[string]string
	Outputs map[string]string
}
