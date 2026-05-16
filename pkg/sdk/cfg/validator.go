package cfg

// Validator runs after decode to check that a value meets a module
// author's constraints. Describe returns a structured form so the
// schema commands and editor tooling can render the constraint
// declaratively rather than as an opaque function.
type Validator interface {
	Check(any) error
	Describe() ValidatorDesc
}

// ValidatorDesc names a validator's kind and parameters. Standard
// kinds include "pattern", "range", "length", "enum", "all", and
// "custom"; introspection tools render the known kinds to their UB
// modifier and fall back to the description text for "custom".
type ValidatorDesc struct {
	Kind   string
	Params map[string]any
}
