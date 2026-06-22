package runtime

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// LibrarySchema describes a Go library's registered resource, data
// source, and action types as the dev CLI sees them at compile time.
// Each entry is keyed by the type's kebab-case name (the same name
// used in factory source).
type LibrarySchema struct {
	Resources   map[string]*TypeSchema
	DataSources map[string]*TypeSchema
	Actions     map[string]*TypeSchema
	// Functions maps each function name the library exports to its
	// declared signature. The implementations live in the compiled Go
	// and cannot run at compile time, but the signature lets the
	// reference checker reject a call to an unknown function or one
	// given the wrong number of arguments, and the inferrer check each
	// argument's type and use the result type. A function registered
	// without declared types reads as all-Unknown, which counts
	// arguments but checks no types.
	Functions map[string]typecheck.FuncSig

	// Configuration describes the fields of the library's Configuration
	// struct, keyed by kebab-case field name. Nil when the library
	// declares no configuration or when the struct behind the
	// ConfigurationType's New function cannot be read from source.
	// HasConfiguration distinguishes the two: it is true whenever the
	// library declares a configuration, readable or not, so checks can
	// tell "no configuration" from "fields unknowable".
	Configuration         map[string]typecheck.Type
	ConfigurationFields   []typecheck.ObjectField
	ConfigurationDefaults []lang.DefaultSpec
	ConfigurationDigest   string
	ConfigurationEmpty    bool
	HasConfiguration      bool
}

// TypeSchema describes the input and output fields of one resource,
// data source, or action. Each map keys a kebab-case field name (the
// form factory source uses) to that field's semantic Type. Inputs
// lists the receiver type's exported fields; Outputs lists the
// output struct's. The walker that builds this schema (goschema)
// recursively expands named struct types so nested object fields
// can be type-checked too.
//
// SensitiveInputs and SensitiveOutputs hold the kebab-case names of
// fields a library marked sensitive via a `ub:",sensitive"` struct
// tag. Both are top-level only; sensitivity does not descend into
// nested object fields.
type TypeSchema struct {
	Inputs           map[string]typecheck.Type
	Outputs          map[string]typecheck.Type
	SensitiveInputs  []string
	SensitiveOutputs []string

	// Constraints holds the type's cross-field constraints, derived from
	// its Constraints method at compile time, in the embeddable string
	// form. A check parses them with lang.ParseSpecs and runs them through
	// lang.CheckConstraintEntries, the same path UB constraints take.
	Constraints []lang.ConstraintSpec

	// Defaults holds the type's declared input defaults, derived from
	// its Defaults method at compile time. A field with a Value default
	// is filled in when a body leaves it out; an Optional marker only
	// declares that absence is fine. Any other input is required.
	Defaults []lang.DefaultSpec
}

// ForType returns the schema for a node kind's type, or nil when the
// kind is not a resource, data source, or action or the type is absent.
func (s *LibrarySchema) ForType(kind NodeKind, typ string) *TypeSchema {
	switch kind {
	case NodeResource:
		return s.Resources[typ]
	case NodeDataSource:
		return s.DataSources[typ]
	case NodeAction:
		return s.Actions[typ]
	default:
		return nil
	}
}
