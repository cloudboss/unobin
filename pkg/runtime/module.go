package runtime

import (
	"context"
	"errors"
)

// Module is the registration record a Go module exports for its primitive
// types and actions. The compiler links each imported Go module's Module()
// function and aggregates the resulting registrations under the alias the
// stack source assigned to the import.
type Module struct {
	Name        string
	Description string
	Actions     map[string]ActionType
	Resources   map[string]ResourceType
	DataSources map[string]DataSourceType
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
