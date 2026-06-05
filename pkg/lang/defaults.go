package lang

// DefaultSpec is the embeddable form of one declared input default.
// Field is the input's var reference (var.mode, var.code.timeout),
// the spelling every other field reference uses. A Value default
// holds the default as unobin literal source, parsed and evaluated
// where it is applied; an Optional marker leaves Value empty and sets
// Optional, declaring the field may be absent with nothing filled in.
// goschema produces these from a Go type's Defaults method, and
// codegen bakes them into the factory.
type DefaultSpec struct {
	Field    string
	Value    string
	Optional bool
}
