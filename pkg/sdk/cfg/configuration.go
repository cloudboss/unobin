package cfg

// ConfigurationType registers a library's configuration. New returns
// a pointer to a fresh configuration struct. The decoder populates
// its fields from the operator's config.ub. The struct's fields
// must be wrapper types from this package or nested structs whose
// fields are wrapper types; the schema walker rejects any other
// field type at library load. Description appears in the schema
// commands.
type ConfigurationType struct {
	Description string
	New         func() any
}
