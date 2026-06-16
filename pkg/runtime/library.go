package runtime

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// Library is the registration record a library exports for its types,
// actions, and data sources. Go libraries supply Resources, Actions,
// and DataSources via the generic helpers (`MakeResource` and
// friends); UB libraries - compiled to Go packages by `unobin compile`
// - contribute Composites whose bodies are parsed AST literals built
// into the binary. The compiler links each imported library's record
// and aggregates them under the alias the calling source assigned to
// the import.
type Library struct {
	Name          string
	Description   string
	Configuration *cfg.ConfigurationType
	Actions       map[string]ActionRegistration
	Resources     map[string]ResourceRegistration
	DataSources   map[string]DataSourceRegistration
	// Composites are kept in one map per kind, mirroring the
	// Resources / DataSources / Actions Go-type maps. resource, data,
	// and action are distinct namespaces, so a library may declare
	// resource.foo and data.foo as separate composites.
	ResourceComposites map[string]*CompositeType
	DataComposites     map[string]*CompositeType
	ActionComposites   map[string]*CompositeType
	Functions          map[string]FunctionType
	// Schema carries the library's resource, data source, and action
	// output field sets. Populated by the dev CLI from a fetched Go
	// library's source for compile-time reference checking; nil at
	// runtime since the generated binary does not need it.
	Schema *LibrarySchema

	// Constraints holds each Go type's cross-field constraints in the
	// embeddable spec form, keyed by "<kind>.<type>" (e.g. "resource.vpc")
	// since resource, data, and action are distinct namespaces. codegen
	// sets it in the generated main.go from the constraints goschema
	// derived from the library's source; the plan checks a node against
	// Constraints[node.Kind + "." + node.Type]. UB composites carry their
	// own constraints in their bodies, so this stays empty for UB libraries.
	Constraints map[string][]lang.ConstraintSpec

	// Defaults holds each Go type's declared input defaults, keyed by
	// "<kind>.<type>" like Constraints and set the same way by codegen.
	// The runtime fills a Value default into a body's inputs wherever a
	// field is left out or null, before constraints, triggers, and
	// decode read them; an Optional marker fills nothing.
	Defaults map[string][]lang.DefaultSpec
}

// FunctionType registers a callable function under a Go library. Functions
// take pre-evaluated argument values and return a single value or an
// error. They run inline during expression evaluation and have no DAG
// node or state of their own.
//
// ArgCount and Variadic declare the argument count the compiler enforces at
// each call site: a non-variadic function takes exactly ArgCount arguments,
// a variadic one takes ArgCount or more. A call's argument count is fixed in
// the source, so this check is compile-time only and the function body may
// assume it already holds.
type FunctionType struct {
	Name        string
	Description string
	ArgCount    int
	Variadic    bool
	Func        func(args []any) (any, error)
}

// CompositeType registers a UB-implemented type under a library. Body
// is the generic body file for the composite. SyntaxBody is the typed body
// used by grammar-first graph extraction when it is available.
//
// Libraries is the resolved import table for this composite's body,
// keyed by the alias declared in the body's `imports:` block. The
// runtime looks up composite-internal nodes against this table, not
// the stack root's, so a composite can be reused without the caller
// importing every library it transitively uses. A nil Libraries falls
// back to the executor's root Libraries table for backward
// compatibility with composites built directly in tests.
type CompositeType struct {
	Name       string
	Kind       NodeKind
	Body       *lang.File
	SyntaxBody *syntax.FactoryBody
	Libraries  map[string]*Library
}

// Composite returns the composite of the given kind and name, or nil
// when the library has none. resource, data, and action are independent
// namespaces, so the kind selects which map to consult.
func (l *Library) Composite(kind NodeKind, name string) *CompositeType {
	switch kind {
	case NodeData:
		return l.DataComposites[name]
	case NodeAction:
		return l.ActionComposites[name]
	default:
		return l.ResourceComposites[name]
	}
}

// AddComposite stores ct in the map for its kind, creating the map on
// first use.
func (l *Library) AddComposite(ct *CompositeType) {
	switch ct.Kind {
	case NodeData:
		if l.DataComposites == nil {
			l.DataComposites = map[string]*CompositeType{}
		}
		l.DataComposites[ct.Name] = ct
	case NodeAction:
		if l.ActionComposites == nil {
			l.ActionComposites = map[string]*CompositeType{}
		}
		l.ActionComposites[ct.Name] = ct
	default:
		if l.ResourceComposites == nil {
			l.ResourceComposites = map[string]*CompositeType{}
		}
		l.ResourceComposites[ct.Name] = ct
	}
}

// ErrNotFound is returned by a resource's Read method when the
// resource is absent in the cloud. The runtime treats it as a
// request to recreate.
var ErrNotFound = errors.New("resource not found")

// migrateEntry upgrades a prior state entry from an older schema version
// to the resource type's current one by calling the registration's
// Migrate. Both halves -- inputs and outputs -- are upgraded together so
// the rewritten entry can be stamped at the current version without
// leaving the inputs at the old version. Returns the entry unchanged
// when versions match.
func migrateEntry(
	reg ResourceRegistration, alias string, priorVersion int, prior MigrationState,
) (MigrationState, error) {
	current := reg.SchemaVersion()
	if priorVersion >= current {
		return prior, nil
	}
	out, err := reg.Migrate(priorVersion, prior)
	if err != nil {
		blameLibrary(err, alias)
		return MigrationState{}, err
	}
	return out, nil
}
