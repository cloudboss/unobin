package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Module is the registration record a module exports for its types,
// actions, and data sources. Go modules supply Resources, Actions,
// and DataSources directly; UB modules - compiled to Go packages by
// `unobin compile` - contribute Composites whose bodies are parsed
// AST literals built into the binary. The compiler links each
// imported module's record and aggregates them under the alias the
// calling source assigned to the import.
type Module struct {
	Name        string
	Description string
	Actions     map[string]ActionType
	Resources   map[string]ResourceType
	DataSources map[string]DataSourceType
	Composites  map[string]*CompositeType
}

// CompositeType registers a UB-implemented type under a module. Body
// is the parsed body file for the composite (same shape as a stack
// minus `configurations:`). The runtime expands a call site into
// sub-DAG nodes by walking Body's `resources:`, `actions:`, and
// `data:` blocks under the call site's address prefix.
type CompositeType struct {
	Name string
	Body *lang.File
}

// ActionType registers an action under a module. Name is the type name as
// referenced in stack source (`actions: { core: { command: { ... } } }`
// dispatches to the `command` ActionType under the `core` module). New
// returns a fresh, zeroed Action; the runtime fills in its inputs and
// then calls Run.
type ActionType struct {
	Name        string
	Description string
	New         func() Action
}

// Action is the interface a Go implementation satisfies. The runtime
// instantiates a fresh Action via ActionType.New, decodes the action's
// input fields onto it, then calls Run. The returned value is encoded
// into state and made available to other DAG nodes as
// `action.<module>.<type>.<name>.<field>`.
type Action interface {
	Run(ctx context.Context) (any, error)
}

// ResourceType registers a primitive resource under a module. SchemaVersion
// is the resource's current state schema; bumping it requires shipping a
// Migrate function that upgrades older state values to the new shape.
type ResourceType struct {
	Name          string
	Description   string
	SchemaVersion int
	New           func() Resource
	Migrate       func(oldVersion int, oldState map[string]any) (map[string]any, error)
}

// Resource is the lifecycle interface a primitive resource satisfies. The
// runtime instantiates a fresh value via ResourceType.New, decodes the
// resource's input fields onto it, and dispatches to Create, Read, Update,
// or Delete based on the plan. priorOutputs carries the outputs of the
// previous successful Create or Update from state; pass nil on Create.
//
// ReplaceFields names the input fields whose change requires a destroy
// followed by a fresh Create rather than an in-place Update. The runtime
// uses this to compute replace-because chains in plan output.
type Resource interface {
	Create(ctx context.Context) (any, error)
	Read(ctx context.Context, priorOutputs any) (any, error)
	Update(ctx context.Context, priorOutputs any) (any, error)
	Delete(ctx context.Context, priorOutputs any) error
	ReplaceFields() []string
}

// ErrNotFound is returned by Resource.Read when the resource is absent in the
// cloud. The runtime treats it as a request to recreate.
var ErrNotFound = errors.New("resource not found")

// migrateOutputs upgrades a state entry's outputs from an older schema
// version to the resource type's current one by calling rt.Migrate.
// Returns the outputs unchanged when versions match. Errors when the
// stored version is older but no Migrate function is registered.
func migrateOutputs(rt ResourceType, priorVersion int, outputs map[string]any) (map[string]any, error) {
	if priorVersion >= rt.SchemaVersion {
		return outputs, nil
	}
	if rt.Migrate == nil {
		return nil, fmt.Errorf(
			"state schema-version is %d but the current module declares %d with no Migrate function",
			priorVersion, rt.SchemaVersion)
	}
	return rt.Migrate(priorVersion, outputs)
}

// DataSourceType registers a readonly data source under a module.
type DataSourceType struct {
	Name        string
	Description string
	New         func() DataSource
}

// DataSource is the readonly counterpart to Resource. Every plan reruns
// Read; no state is kept between runs. The receiver carries the data
// source's input fields once the runtime has decoded them.
type DataSource interface {
	Read(ctx context.Context) (any, error)
}
