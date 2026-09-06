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
// Inputs and Outputs are the recorded values after any required schema
// migration. Invalid recorded values fail before Update is called.
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

// NoConfig is the config parameter for libraries that declare no config.
type NoConfig struct{}

// TypedResource is the typed contract a library author implements for
// one primitive resource type. In names the input struct, which is the
// method receiver; Out must be a pointer to an output struct (e.g.
// *VpcOutput) so a call without prior state passes nil. Config names the
// decoded library config type. Update receives a Prior bundling the
// last apply's inputs and outputs.
type TypedResource[In, Out, Config any] interface {
	Create(ctx context.Context, config Config) (Out, error)
	Read(ctx context.Context, config Config, prior Out) (Out, error)
	Update(ctx context.Context, config Config, prior Prior[In, Out]) (Out, error)
	Delete(ctx context.Context, config Config, prior Out) error
}

// TypedAction is the typed contract for actions. Out names the
// action's output struct.
type TypedAction[Out, Config any] interface {
	Run(ctx context.Context, config Config) (Out, error)
}

// TypedDataSource is the typed contract for read-only data sources.
type TypedDataSource[Out, Config any] interface {
	Read(ctx context.Context, config Config) (Out, error)
}

// ResourceRegistration stores a validated definition and its typed provider
// operations. Library authors create registrations with MakeResource.
type ResourceRegistration interface {
	resourceDefinition() *resourceDefinitionRegistration
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
type resourcePtr[T, Out, Config any] interface {
	*T
	TypedResource[T, Out, Config]
}

type actionPtr[T, Out, Config any] interface {
	*T
	TypedAction[Out, Config]
}

type dataSourcePtr[T, Out, Config any] interface {
	*T
	TypedDataSource[Out, Config]
}

// MakeResource validates the definition and registers the lifecycle implemented
// by *T. Invalid definitions panic during registration. Each receiver starts
// as new(T) before the runtime decodes its inputs.
func MakeResource[T, Out, Config any, PT resourcePtr[T, Out, Config]](
	definition ResourceDefinition[T, Out, Config],
) ResourceRegistration {
	return MakeResourceWith[T, Out, Config, PT](definition, nil)
}

// MakeResourceWith is the variant of MakeResource for callers that
// need each receiver to capture external state. The constructor runs
// once per instance the runtime needs; Decode then fills it from the
// inputs. Invalid definitions panic before the constructor can run.
func MakeResourceWith[T, Out, Config any, PT resourcePtr[T, Out, Config]](
	definition ResourceDefinition[T, Out, Config],
	construct func() *T,
) ResourceRegistration {
	resolved, err := resolveResourceDefinition(definition)
	if err != nil {
		panic(fmt.Errorf("resource definition: %w", err))
	}
	return resourceReg{
		definition: newResolvedResourceDefinitionRegistration[T, Out, Config, PT](
			resolved, construct,
		),
	}
}

// MakeAction produces an ActionRegistration that wraps a
// TypedAction[Out, Config] implemented by *T.
func MakeAction[T, Out, Config any, PT actionPtr[T, Out, Config]]() ActionRegistration {
	return typedActionReg[T, Out, Config, PT]{}
}

// MakeActionWith is the variant of MakeAction that captures
// external state through the constructor.
func MakeActionWith[T, Out, Config any, PT actionPtr[T, Out, Config]](
	construct func() *T,
) ActionRegistration {
	return typedActionReg[T, Out, Config, PT]{construct: construct}
}

// MakeDataSource produces a DataSourceRegistration that wraps a
// TypedDataSource[Out, Config] implemented by *T.
func MakeDataSource[T, Out, Config any, PT dataSourcePtr[T, Out, Config]]() DataSourceRegistration {
	return typedDataSourceReg[T, Out, Config, PT]{}
}

// MakeDataSourceWith is the variant of MakeDataSource that captures
// external state through the constructor.
func MakeDataSourceWith[T, Out, Config any, PT dataSourcePtr[T, Out, Config]](
	construct func() *T,
) DataSourceRegistration {
	return typedDataSourceReg[T, Out, Config, PT]{construct: construct}
}

type resourceReg struct {
	definition *resourceDefinitionRegistration
}

func (r resourceReg) resourceDefinition() *resourceDefinitionRegistration {
	return r.definition
}

type typedActionReg[T, Out, Config any, PT actionPtr[T, Out, Config]] struct {
	construct func() *T
}

func (r typedActionReg[T, Out, Config, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedActionReg[T, Out, Config, PT]) Run(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	return guard("running this action", false, func() (Out, error) {
		return PT(receiver.(*T)).Run(ctx, config)
	})
}

func (typedActionReg[T, Out, Config, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

type typedDataSourceReg[T, Out, Config any, PT dataSourcePtr[T, Out, Config]] struct {
	construct func() *T
}

func (r typedDataSourceReg[T, Out, Config, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedDataSourceReg[T, Out, Config, PT]) Read(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	return guard("reading this data source", false, func() (Out, error) {
		return PT(receiver.(*T)).Read(ctx, config)
	})
}

func (typedDataSourceReg[T, Out, Config, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
}

func coerceConfig[Config any](cfg any) (Config, error) {
	var zero Config
	if cfg == nil {
		return zero, nil
	}
	config, ok := cfg.(Config)
	if !ok {
		return zero, fmt.Errorf("config type mismatch: expected %T, got %T", zero, cfg)
	}
	return config, nil
}
