package runtime

import "context"

// Module is the registration record a Go module exports for its primitive
// types and actions. The compiler links each imported Go module's Module()
// function and aggregates the resulting registrations under the alias the
// stack source assigned to the import.
type Module struct {
	Name        string
	Description string
	Actions     map[string]ActionType
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
