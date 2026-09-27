package runtime

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

// Prior describes the recorded provider target and the latest observation.
type Prior[In, Out, Config any] struct {
	Inputs        In
	Outputs       Out
	Observed      Out
	Configuration Config
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

// TypedResource is the provider operation contract for one resource type.
type TypedResource[In, Out, Config any] interface {
	Create(ctx context.Context, config Config) (Out, error)
	Read(ctx context.Context, config Config, prior Prior[In, Out, Config]) (Out, error)
	Update(ctx context.Context, config Config, prior Prior[In, Out, Config]) (Out, error)
	Delete(ctx context.Context, config Config, prior Prior[In, Out, Config]) error
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

// MigrationState is the pair of persisted maps a resource migration upgrades:
// the inputs evaluated during the last apply and the outputs returned then.
// Observed (see Prior) is plan-time only and is never persisted, so a migration
// covers inputs and outputs alone.
type MigrationState struct {
	Inputs  map[string]any
	Outputs map[string]any
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
	Read(ctx context.Context, receiver, cfg any, prior resourcePrior) (any, error)
	Update(ctx context.Context, receiver, cfg any, prior resourcePrior) (any, error)
	ValidateInputs(ctx context.Context, receiver, cfg any) error
	Delete(ctx context.Context, receiver, cfg any, prior resourcePrior) error
	InputsEqual(receiver any, priorInputs map[string]any) (bool, error)
	KnownInputsEqual(receiver any, priorInputs map[string]any, pending map[string][]string) (bool, error)
	ReplacementReasons(receiver, cfg any, prior resourcePrior) ([]string, error)
	KnownReplacementReasons(
		receiver, cfg any, prior resourcePrior, pending map[string][]string, configPending bool,
	) ([]string, error)
	PendingReplacementReasons(unresolved map[string][]string, configPending bool) []string
	StableID(inputs, outputs map[string]any) (*string, error)
	HasStableID() bool
	OutputType() reflect.Type
}

type resourcePrior struct {
	Inputs        map[string]any
	Outputs       map[string]any
	Observed      map[string]any
	Configuration map[string]any
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

// MakeResource registers a typed resource with its lifecycle definition.
func MakeResource[T, Out, Config any, PT resourcePtr[T, Out, Config]](
	definition ResourceDefinition[T, Out, Config],
) ResourceRegistration {
	return typedResourceReg[T, Out, Config, PT]{
		definition: mustResolveResourceDefinition(definition),
	}
}

// MakeResourceWith is the variant of MakeResource for callers that
// need each receiver to capture external state. The constructor runs
// once per instance the runtime needs; Decode then fills it from the
// inputs.
func MakeResourceWith[T, Out, Config any, PT resourcePtr[T, Out, Config]](
	definition ResourceDefinition[T, Out, Config],
	construct func() *T,
) ResourceRegistration {
	if construct == nil {
		panic("resource constructor is nil")
	}
	return typedResourceReg[T, Out, Config, PT]{
		definition: mustResolveResourceDefinition(definition),
		construct:  construct,
	}
}

func mustResolveResourceDefinition[In, Out, Config any](
	definition ResourceDefinition[In, Out, Config],
) resolvedResourceDefinition[In, Out, Config] {
	resolved, err := resolveResourceDefinition(definition)
	if err != nil {
		panic(err)
	}
	return resolved
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

type typedResourceReg[T, Out, Config any, PT resourcePtr[T, Out, Config]] struct {
	definition resolvedResourceDefinition[T, Out, Config]
	construct  func() *T
}

func (r typedResourceReg[T, Out, Config, PT]) SchemaVersion() int {
	return r.definition.schemaVersion
}

func (r typedResourceReg[T, Out, Config, PT]) Migrate(
	old int, prior MigrationState,
) (MigrationState, error) {
	if r.definition.migrate == nil {
		return MigrationState{}, fmt.Errorf("no migration registered for version %d", old)
	}
	return guard("migrating this resource's state", false, func() (MigrationState, error) {
		return r.definition.migrate(old, prior)
	})
}

func (r typedResourceReg[T, Out, Config, PT]) NewReceiver() any {
	if r.construct != nil {
		return r.construct()
	}
	return new(T)
}

func (typedResourceReg[T, Out, Config, PT]) Create(
	ctx context.Context, receiver, cfg any,
) (any, error) {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	result, err := guard("creating this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Create(ctx, config)
	})
	if err != nil {
		return nil, err
	}
	return result, validateResourceOutput(result)
}

func (typedResourceReg[T, Out, Config, PT]) Read(
	ctx context.Context, receiver, cfg any, rawPrior resourcePrior,
) (any, error) {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	prior, err := makeTypedPrior[T, Out, Config](rawPrior)
	if err != nil {
		return nil, err
	}
	result, err := guard("reading this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Read(ctx, config, prior)
	})
	if err != nil {
		return nil, err
	}
	return result, validateResourceOutput(result)
}

func (typedResourceReg[T, Out, Config, PT]) Update(
	ctx context.Context, receiver, cfg any, rawPrior resourcePrior,
) (any, error) {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	prior, err := makeTypedPrior[T, Out, Config](rawPrior)
	if err != nil {
		return nil, err
	}
	result, err := guard("updating this resource", false, func() (Out, error) {
		return PT(receiver.(*T)).Update(ctx, config, prior)
	})
	if err != nil {
		return nil, err
	}
	return result, validateResourceOutput(result)
}

func (r typedResourceReg[T, Out, Config, PT]) ValidateInputs(
	ctx context.Context, receiver, cfg any,
) error {
	if r.definition.validate == nil {
		return nil
	}
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return err
	}
	return guardErr("validating this resource's inputs", false, func() error {
		return r.definition.validate(ctx, *receiver.(*T), config)
	})
}

func (typedResourceReg[T, Out, Config, PT]) Delete(
	ctx context.Context, receiver, cfg any, rawPrior resourcePrior,
) error {
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return err
	}
	prior, err := makeTypedPrior[T, Out, Config](rawPrior)
	if err != nil {
		return err
	}
	return guardErr("deleting this resource", false, func() error {
		return PT(receiver.(*T)).Delete(ctx, config, prior)
	})
}

func (r typedResourceReg[T, Out, Config, PT]) InputsEqual(
	receiver any, priorInputs map[string]any,
) (bool, error) {
	return r.KnownInputsEqual(receiver, priorInputs, nil)
}

func (r typedResourceReg[T, Out, Config, PT]) KnownInputsEqual(
	receiver any, priorInputs map[string]any, pending map[string][]string,
) (bool, error) {
	prior, err := coerceResourceInputs[T](priorInputs)
	if err != nil {
		return false, err
	}
	return r.definition.knownInputsEqual(prior, *receiver.(*T), pending)
}

func (r typedResourceReg[T, Out, Config, PT]) ReplacementReasons(
	receiver, cfg any, rawPrior resourcePrior,
) ([]string, error) {
	return r.KnownReplacementReasons(receiver, cfg, rawPrior, nil, false)
}

func (r typedResourceReg[T, Out, Config, PT]) KnownReplacementReasons(
	receiver, cfg any, rawPrior resourcePrior, pending map[string][]string, configPending bool,
) ([]string, error) {
	prior, err := makeTypedPrior[T, Out, Config](rawPrior)
	if err != nil {
		return nil, err
	}
	config, err := coerceConfig[Config](cfg)
	if err != nil {
		return nil, err
	}
	return r.definition.knownReplacementReasons(
		prior.Inputs,
		*receiver.(*T),
		prior.Configuration,
		config,
		prior.Outputs,
		prior.Observed,
		pending,
		configPending,
	)
}

func (r typedResourceReg[T, Out, Config, PT]) PendingReplacementReasons(
	unresolved map[string][]string,
	configPending bool,
) []string {
	var reasons []string
	for _, field := range r.definition.replacementFields {
		if _, pending := unresolved[topLevelPath(field.path)]; pending {
			reasons = append(reasons, field.path)
		}
	}
	for _, rule := range r.definition.replacementRules {
		if _, pending := unresolved[topLevelPath(rule.field.path)]; pending {
			reasons = append(reasons, rule.field.path)
		}
	}
	if configPending {
		for _, field := range r.definition.configurationFields {
			reasons = append(reasons, field.path)
		}
		for _, rule := range r.definition.driftRules {
			reasons = append(reasons, rule.field.path)
		}
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}

func topLevelPath(path string) string {
	if i := strings.IndexByte(path, '.'); i >= 0 {
		return path[:i]
	}
	return path
}

func (r typedResourceReg[T, Out, Config, PT]) StableID(
	inputs, outputs map[string]any,
) (*string, error) {
	if r.definition.stableID == nil {
		return nil, nil
	}
	typedInputs, err := coerceResourceInputs[T](inputs)
	if err != nil {
		return nil, err
	}
	typedOutputs, err := coercePrior[Out](outputs)
	if err != nil {
		return nil, err
	}
	id, err := guard("deriving this resource's stable ID", false, func() (string, error) {
		return r.definition.stableID(typedInputs, typedOutputs)
	})
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("stable ID is empty")
	}
	return &id, nil
}

func (r typedResourceReg[T, Out, Config, PT]) HasStableID() bool {
	return r.definition.stableID != nil
}

func (typedResourceReg[T, Out, Config, PT]) OutputType() reflect.Type {
	var zero Out
	return reflect.TypeOf(zero)
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

func makeTypedPrior[In, Out, Config any](
	prior resourcePrior,
) (Prior[In, Out, Config], error) {
	inputs, err := coerceResourceInputs[In](prior.Inputs)
	if err != nil {
		return Prior[In, Out, Config]{}, diagnostic.Context("prior inputs", err)
	}
	outputs, err := coercePrior[Out](prior.Outputs)
	if err != nil {
		return Prior[In, Out, Config]{}, diagnostic.Context("prior outputs", err)
	}
	observed, err := coercePrior[Out](prior.Observed)
	if err != nil {
		return Prior[In, Out, Config]{}, diagnostic.Context("observed outputs", err)
	}
	configuration, err := coerceRecordedConfiguration[Config](prior.Configuration)
	if err != nil {
		return Prior[In, Out, Config]{}, diagnostic.Context("prior configuration", err)
	}
	return Prior[In, Out, Config]{
		Inputs:        inputs,
		Outputs:       outputs,
		Observed:      observed,
		Configuration: configuration,
	}, nil
}

func coerceResourceInputs[In any](inputs map[string]any) (In, error) {
	var zero In
	target := reflect.New(reflect.TypeFor[In]())
	if err := Decode(target.Interface(), inputs); err != nil {
		return zero, diagnostic.Context("decode resource inputs", err)
	}
	return target.Elem().Interface().(In), nil
}

func coerceRecordedConfiguration[Config any](value any) (Config, error) {
	var zero Config
	if value == nil {
		return zero, nil
	}
	if typed, ok := value.(Config); ok {
		return typed, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return zero, fmt.Errorf("configuration type mismatch: expected %T, got %T", zero, value)
	}
	t := reflect.TypeFor[Config]()
	if t.Kind() == reflect.Pointer {
		target := reflect.New(t.Elem())
		if err := Decode(target.Interface(), m); err != nil {
			return zero, err
		}
		return target.Interface().(Config), nil
	}
	target := reflect.New(t)
	if err := Decode(target.Interface(), m); err != nil {
		return zero, err
	}
	return target.Elem().Interface().(Config), nil
}

func validateResourceOutput[Out any](output Out) error {
	value := reflect.ValueOf(output)
	if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return fmt.Errorf("resource returned a nil output")
	}
	return nil
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
	priorValue := reflect.ValueOf(prior)
	switch priorValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		if priorValue.IsNil() {
			return zero, nil
		}
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
		return zero, diagnostic.Context(fmt.Sprintf("coerce prior state into %s", t), err)
	}
	return target.Interface().(Out), nil
}
