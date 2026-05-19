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

// TypeSchema describes the output fields of one resource, data
// source, or action. Outputs maps each kebab-case field name (the
// key by which stack source addresses the field, e.g. the `id` in
// `resource.aws.vpc.main.id`) to the field's Go type expression as
// written in source (e.g. `string`, `[]string`, `time.Duration`).
type TypeSchema struct {
	Outputs map[string]string
}
