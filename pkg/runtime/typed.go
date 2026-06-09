package runtime

import (
	"context"
	"fmt"
	"reflect"
)

// Prior is everything known before Update acts: the inputs the body
// evaluated to on the last apply, the outputs the resource returned
// then, and the reality a plan-time Read last saw. Compare current
// inputs against Inputs with Changed to decide what to reconcile, read
// Outputs for the prior handle (an id, an arn) the update acts against,
// and read Observed to patch from current reality rather than the
// recorded result when the two have drifted apart.
//
// Inputs is the input struct by value, so it is never nil and needs no
// guard; a prior whose recorded inputs no longer decode into the
// current struct degrades to the zero value, which reads as "every
// field changed". Outputs and Observed keep the same pointer type the
// rest of the contract uses, so a resource with no recorded result or
// no plan-time read still passes nil.
//
// Observed is plan-time, not apply-time: apply does not re-Read before
// Update, so between plan and apply reality can move further. A resource
// that needs apply-time truth must Read itself.
type Prior[In, Out any] struct {
	Inputs   In
	Outputs  Out
	Observed Out
}

// Changed reports whether a field differs between its prior and current
// value. It compares by value, so a pointer field compares what it
// points at, and a state round trip that re-decodes an equal value is
// not a false positive.
func Changed[T any](prior, current T) bool {
	return !reflect.DeepEqual(prior, current)
}

// TypedResource is the typed contract a library author implements for
// one primitive resource type. In names the input struct, which is the
// method receiver; Out names the output struct, usually a pointer (e.g.
// *VpcOutput) so a "no prior state" call passes nil. Update receives a
// Prior bundling the last apply's inputs and outputs.
type TypedResource[In, Out any] interface {
	SchemaVersion() int
	Create(ctx context.Context, cfg any) (Out, error)
	Read(ctx context.Context, cfg any, prior Out) (Out, error)
	Update(ctx context.Context, cfg any, prior Prior[In, Out]) (Out, error)
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

// MigrationState is the pair of persisted maps a Migrator upgrades: the
// inputs the body evaluated to on the last apply and the outputs the
// resource returned then. Observed (see Prior) is plan-time only and is
// never persisted, so a migration covers inputs and outputs alone.
type MigrationState struct {
	Inputs  map[string]any
	Outputs map[string]any
}

// Migrator is an optional add-on a TypedResource may implement when its
// SchemaVersion has incremented past 1 and an older state entry needs
// upgrading. Migrate receives the whole recorded entry at the version it
// was written and returns it at the current version. Upgrading both
// halves together keeps the entry's single SchemaVersion stamp correct:
// were only the outputs upgraded, the entry would be stamped current
// while its inputs stayed at the old version, and a later input
// migration would never run.
type Migrator interface {
	Migrate(oldVersion int, prior MigrationState) (MigrationState, error)
}

// ResourceRegistration is the type-erased registration the runtime's
// resource map holds. A library author produces one via MakeResource;
// the runtime calls the methods on it to dispatch CRUD work without
// caring about the typed Out parameter.
type ResourceRegistration interface {
	SchemaVersion() int
	Migrate(oldVersion int, prior MigrationState) (MigrationState, error)
	NewReceiver() any
	Create(ctx context.Context, receiver, cfg any) (any, error)
	Read(ctx context.Context, receiver, cfg, prior any) (any, error)
	Update(ctx context.Context, receiver, cfg, priorInputs, priorOutputs, observed any) (any, error)
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

// resourcePtr constrains PT to be exactly *T and a TypedResource whose
// input struct is T. The dev CLI uses this pattern so the MakeResource
// helper can call methods on *T without the caller spelling out the
// pointer type.
type resourcePtr[T any, Out any] interface {
	*T
	TypedResource[T, Out]
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
// `runtime.MakeResource[Vpc, *VpcOutput]()`. Each receiver is
// zero-constructed via new(T) when the runtime asks for one.
func MakeResource[T any, Out any, PT resourcePtr[T, Out]]() ResourceRegistration {
	return typedResourceReg[T, Out, PT]{}
}

// MakeResourceWith is the variant of MakeResource for callers that
// need each receiver to capture external state. The constructor runs
// once per instance the runtime needs; Decode then fills it from the
// inputs.
func MakeResourceWith[T any, Out any, PT resourcePtr[T, Out]](
	construct func() *T,
) ResourceRegistration {
	return typedResourceReg[T, Out, PT]{construct: construct}
}

// MakeAction produces an ActionRegistration that wraps a
// TypedAction[Out] implemented by *T.
func MakeAction[T any, Out any, PT actionPtr[T, Out]]() ActionRegistration {
	return typedActionReg[T, Out, PT]{}
}

// MakeActionWith is the variant of MakeAction that captures
// external state through the constructor.
func MakeActionWith[T any, Out any, PT actionPtr[T, Out]](
	construct func() *T,
) ActionRegistration {
	return typedActionReg[T, Out, PT]{construct: construct}
}

// MakeDataSource produces a DataSourceRegistration that wraps a
// TypedDataSource[Out] implemented by *T.
func MakeDataSource[T any, Out any, PT dataSourcePtr[T, Out]]() DataSourceRegistration {
	return typedDataSourceReg[T, Out, PT]{}
}

// MakeDataSourceWith is the variant of MakeDataSource that captures
// external state through the constructor.
func MakeDataSourceWith[T any, Out any, PT dataSourcePtr[T, Out]](
	construct func() *T,
) DataSourceRegistration {
	return typedDataSourceReg[T, Out, PT]{construct: construct}
}

type typedResourceReg[T any, Out any, PT resourcePtr[T, Out]] struct {
	construct func() *T
}

func (typedResourceReg[T, Out, PT]) SchemaVersion() int {
	return PT(new(T)).SchemaVersion()
}

func (typedResourceReg[T, Out, PT]) Migrate(
	old int, prior MigrationState,
) (MigrationState, error) {
	m, ok := any(PT(new(T))).(Migrator)
	if !ok {
		return MigrationState{}, fmt.Errorf("no migration registered for version %d", old)
	}
	return guard("migrating this resource's state", false, func() (MigrationState, error) {
		return m.Migrate(old, prior)
	})
}

func (r typedResourceReg[T, Out, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedResourceReg[T, Out, PT]) Create(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return guard("creating this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Create(ctx, cfg)
	})
}

func (typedResourceReg[T, Out, PT]) Read(
	ctx context.Context, receiver, cfg, prior any,
) (any, error) {
	p, err := coercePrior[Out](prior)
	if err != nil {
		return nil, err
	}
	return guard("reading this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Read(ctx, cfg, p)
	})
}

func (typedResourceReg[T, Out, PT]) Update(
	ctx context.Context, receiver, cfg, priorInputs, priorOutputs, observed any,
) (any, error) {
	out, err := coercePrior[Out](priorOutputs)
	if err != nil {
		return nil, err
	}
	obs, err := coercePrior[Out](observed)
	if err != nil {
		return nil, err
	}
	prior := Prior[T, Out]{
		Inputs:   coercePriorInputs[T](priorInputs),
		Outputs:  out,
		Observed: obs,
	}
	return guard("updating this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Update(ctx, cfg, prior)
	})
}

func (typedResourceReg[T, Out, PT]) Delete(
	ctx context.Context, receiver, cfg, prior any,
) error {
	p, err := coercePrior[Out](prior)
	if err != nil {
		return err
	}
	return guardErr("deleting this resource", false, func() error {
		return PT(receiver.(*T)).Delete(ctx, cfg, p)
	})
}

func (typedResourceReg[T, Out, PT]) ReplaceFields(receiver any) []string {
	return PT(receiver.(*T)).ReplaceFields()
}

func (typedResourceReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

type typedActionReg[T any, Out any, PT actionPtr[T, Out]] struct {
	construct func() *T
}

func (r typedActionReg[T, Out, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedActionReg[T, Out, PT]) Run(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return guard("running this action", false, func() (Out, error) {
		return PT(receiver.(*T)).Run(ctx, cfg)
	})
}

func (typedActionReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

type typedDataSourceReg[T any, Out any, PT dataSourcePtr[T, Out]] struct {
	construct func() *T
}

func (r typedDataSourceReg[T, Out, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedDataSourceReg[T, Out, PT]) Read(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	return guard("reading this data source", false, func() (Out, error) {
		return PT(receiver.(*T)).Read(ctx, cfg)
	})
}

func (typedDataSourceReg[T, Out, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

// coercePrior returns prior as Out. nil is the runtime's "no prior
// state" sentinel and yields the zero value (a nil pointer for the
// usual Out = *Something). An already-typed Out passes through. State
// loaded from disk arrives as map[string]any (JSON round trip) and
// gets decoded into a fresh Out via the same Decode rules used for
// inputs. A prior value that is neither nil, the typed output, nor
// a decodable map returns an error rather than crashing, so a corrupt
// or hand-edited state entry is reported to the operator like any other
// step failure.
func coercePrior[Out any](prior any) (Out, error) {
	var zero Out
	if prior == nil {
		return zero, nil
	}
	if typed, ok := prior.(Out); ok {
		return typed, nil
	}
	m, ok := prior.(map[string]any)
	if !ok {
		return zero, fmt.Errorf("coerce prior state: unsupported type %T", prior)
	}
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Pointer {
		return zero, fmt.Errorf("coerce prior state: output type %T is not a pointer", zero)
	}
	target := reflect.New(t.Elem())
	if err := Decode(target.Interface(), m); err != nil {
		return zero, fmt.Errorf("coerce prior state into %s: %w", t, err)
	}
	return target.Interface().(Out), nil
}

// coercePriorInputs decodes a prior inputs map into the input struct In
// for comparison inside Update. Unlike prior outputs, prior inputs are
// advisory: they gate an optimization, not the correctness of the
// update, so any problem decoding them (a field removed or retyped
// since the last apply, a hand-edited entry) degrades to the zero
// value, which reads as "every field changed" and triggers a full
// reconcile rather than failing the apply.
func coercePriorInputs[In any](prior any) In {
	var zero In
	if prior == nil {
		return zero
	}
	if typed, ok := prior.(In); ok {
		return typed
	}
	m, ok := prior.(map[string]any)
	if !ok {
		return zero
	}
	target := reflect.New(reflect.TypeOf(zero))
	if err := Decode(target.Interface(), m); err != nil {
		return zero
	}
	return target.Elem().Interface().(In)
}
