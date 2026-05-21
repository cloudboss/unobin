package runtime

import "github.com/cloudboss/unobin/pkg/typecheck"

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
// data source, or action. Each map keys a kebab-case field name (the
// form stack source uses) to that field's semantic Type. Inputs
// lists the receiver type's exported fields; Outputs lists the
// output struct's. The walker that builds this schema (goschema)
// is responsible for producing these Types.
type TypeSchema struct {
	Inputs  map[string]typecheck.Type
	Outputs map[string]typecheck.Type
}
