package runtime

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// Module is the registration record a module exports for its types,
// actions, and data sources. Go modules supply Resources, Actions,
// and DataSources via the generic helpers (`MakeResource` and
// friends); UB modules - compiled to Go packages by `unobin compile`
// - contribute Composites whose bodies are parsed AST literals built
// into the binary. The compiler links each imported module's record
// and aggregates them under the alias the calling source assigned to
// the import.
type Module struct {
	Name          string
	Description   string
	Configuration *cfg.ConfigurationType
	Actions       map[string]ActionRegistration
	Resources     map[string]ResourceRegistration
	DataSources   map[string]DataSourceRegistration
	Composites    map[string]*CompositeType
	Functions     map[string]FunctionType
	StateBackends map[string]state.BackendType
	Encrypters    map[string]encrypt.EncrypterType
	// Schema carries the module's resource, data source, and action
	// output field sets. Populated by the dev CLI from a fetched Go
	// module's source for compile-time reference checking; nil at
	// runtime since the stack binary does not need it.
	Schema *ModuleSchema
}

// FunctionType registers a callable function under a Go module. Functions
// take pre-evaluated argument values and return a single value or an
// error. They run inline during expression evaluation and have no DAG
// node or state of their own.
type FunctionType struct {
	Name        string
	Description string
	Func        func(args []any) (any, error)
}

// CompositeType registers a UB-implemented type under a module. Body
// is the parsed body file for the composite (same shape as a stack
// minus `configurations:`). The runtime expands a call site into
// sub-DAG nodes by walking Body's `resources:`, `actions:`, and
// `data:` blocks under the call site's address prefix.
//
// Modules is the resolved import table for this composite's body,
// keyed by the alias declared in the body's `imports:` block. The
// runtime looks up composite-internal nodes against this table, not
// the stack root's, so a composite can be reused without the caller
// importing every module it transitively uses. A nil Modules falls
// back to the executor's root Modules table for backward
// compatibility with composites built directly in tests.
type CompositeType struct {
	Name    string
	Body    *lang.File
	Modules map[string]*Module
}

// ErrNotFound is returned by a resource's Read method when the
// resource is absent in the cloud. The runtime treats it as a
// request to recreate.
var ErrNotFound = errors.New("resource not found")

// migrateOutputs upgrades a state entry's outputs from an older schema
// version to the resource type's current one by calling the
// registration's Migrate. Returns the outputs unchanged when versions
// match.
func migrateOutputs(
	reg ResourceRegistration, priorVersion int, outputs map[string]any,
) (map[string]any, error) {
	current := reg.SchemaVersion()
	if priorVersion >= current {
		return outputs, nil
	}
	return reg.Migrate(priorVersion, outputs)
}
