package runtime

import (
	"context"
	"fmt"
	"reflect"
)

// TypedResource is the typed contract a module author implements for
// one primitive resource type. Out names the output struct, usually a
// pointer (e.g. *VpcOutput) so a "no prior state" call passes nil.
type TypedResource[Out any] interface {
	SchemaVersion() int
	Create(ctx context.Context, cfg any) (Out, error)
	Read(ctx context.Context, cfg any, prior Out) (Out, error)
	Update(ctx context.Context, cfg any, prior Out) (Out, error)
	Delete(ctx context.Context, cfg any, prior Out) error
	ReplaceFields() []string
}

// TypedAction is the typed contract for actions. Out names the
// action's output struct.
type TypedAction[Out any] interface {
	Run(ctx context.Context, cfg any) (Out, error)
}

// TypedDataSource is the typed contract for read-only data sources.
type TypedDataSource[Out any] interface {
	Read(ctx context.Context, cfg any) (Out, error)
}

// Migrator is an optional add-on a TypedResource may implement when
// its SchemaVersion has incremented past 1 and older state values
// need to be upgraded.
type Migrator interface {
	Migrate(oldVersion int, oldState map[string]any) (map[string]any, error)
}

// ResourceRegistration is the type-erased registration the runtime's
// resource map holds. A module author produces one via MakeResource;
// the runtime calls the methods on it to dispatch CRUD work without
// caring about the typed Out parameter.
type ResourceRegistration interface {
	SchemaVersion() int
	Migrate(oldVersion int, oldState map[string]any) (map[string]any, error)
	NewReceiver() any
	Create(ctx context.Context, receiver, cfg any) (any, error)
	Read(ctx context.Context, receiver, cfg, prior any) (any, error)
	Update(ctx context.Context, receiver, cfg, prior any) (any, error)
	Delete(ctx context.Context, receiver, cfg, prior any) error
	ReplaceFields(receiver any) []string
	OutputType() reflect.Type
}

// ActionRegistration is the type-erased registration for actions.
type ActionRegistration interface {
	NewReceiver() any
	Run(ctx context.Context, receiver, cfg any) (any, error)
	OutputType() reflect.Type
}

// DataSourceRegistration is the type-erased registration for data
// sources.
type DataSourceRegistration interface {
	NewReceiver() any
	Read(ctx context.Context, receiver, cfg any) (any, error)
	OutputType() reflect.Type
}

// resourcePtr constrains PT to be exactly *T and a TypedResource[Out].
// The dev CLI uses this pattern so the MakeResource helper can call
// methods on *T without the caller spelling out the pointer type.
type resourcePtr[T any, Out any] interface {
	*T
	TypedResource[Out]
}

type actionPtr[T any, Out any] interface {
	*T
	TypedAction[Out]
}

type dataSourcePtr[T any, Out any] interface {
	*T
	TypedDataSource[Out]
}

// MakeResource produces a ResourceRegistration that wraps a
// TypedResource[Out] implemented by *T. Use as
// `runtime.MakeResource[Vpc, *VpcOutput]()`.
func MakeResource[T any, Out any, PT resourcePtr[T, Out]]() ResourceRegistration {
	return typedResourceReg[T, Out, PT]{}
}

// MakeAction produces an ActionRegistration that wraps a
// TypedAction[Out] implemented by *T.
func MakeAction[T any, Out any, PT actionPtr[T, Out]]() ActionRegistration {
	return typedActionReg[T, Out, PT]{}
}

// MakeDataSource produces a DataSourceRegistration that wraps a
// TypedDataSource[Out] implemented by *T.
func MakeDataSource[T any, Out any, PT dataSourcePtr[T, Out]]() DataSourceRegistration {
	return typedDataSourceReg[T, Out, PT]{}
}

type typedResourceReg[T any, Out any, PT resourcePtr[T, Out]] struct{}

func (typedResourceReg[T, Out, PT]) SchemaVersion() int {
	return PT(new(T)).SchemaVersion()
}

func (typedResourceReg[T, Out, PT]) Migrate(
	old int, oldState map[string]any,
) (map[string]any, error) {
	if m, ok := any(PT(new(T))).(Migrator); ok {
		return m.Migrate(old, oldState)
	}
	return nil, fmt.Errorf("no migration registered for version %d", old)
}

func (typedResourceReg[T, Out, PT]) NewReceiver() any {
	return new(T)
}

func (typedResourceReg[T, Out, PT]) Create(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return PT(receiver.(*T)).Create(ctx, cfg)
}

func (typedResourceReg[T, Out, PT]) Read(
	ctx context.Context, receiver, cfg, prior any,
) (any, error) {
	return PT(receiver.(*T)).Read(ctx, cfg, coercePrior[Out](prior))
}

func (typedResourceReg[T, Out, PT]) Update(
	ctx context.Context, receiver, cfg, prior any,
) (any, error) {
	return PT(receiver.(*T)).Update(ctx, cfg, coercePrior[Out](prior))
}

func (typedResourceReg[T, Out, PT]) Delete(
	ctx context.Context, receiver, cfg, prior any,
) error {
	return PT(receiver.(*T)).Delete(ctx, cfg, coercePrior[Out](prior))
}

func (typedResourceReg[T, Out, PT]) ReplaceFields(receiver any) []string {
	return PT(receiver.(*T)).ReplaceFields()
}

func (typedResourceReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

type typedActionReg[T any, Out any, PT actionPtr[T, Out]] struct{}

func (typedActionReg[T, Out, PT]) NewReceiver() any {
	return new(T)
}

func (typedActionReg[T, Out, PT]) Run(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return PT(receiver.(*T)).Run(ctx, cfg)
}

func (typedActionReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

type typedDataSourceReg[T any, Out any, PT dataSourcePtr[T, Out]] struct{}

func (typedDataSourceReg[T, Out, PT]) NewReceiver() any {
	return new(T)
}

func (typedDataSourceReg[T, Out, PT]) Read(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return PT(receiver.(*T)).Read(ctx, cfg)
}

func (typedDataSourceReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

// coercePrior returns prior as Out, or the zero value of Out when
// prior is nil. The runtime passes nil for "no prior state" on the
// first read of a new resource; this helper lets the typed methods
// receive a nil pointer (when Out is a pointer type) without an
// explicit nil-cast guard at every call site.
func coercePrior[Out any](prior any) Out {
	if prior == nil {
		var zero Out
		return zero
	}
	return prior.(Out)
}
