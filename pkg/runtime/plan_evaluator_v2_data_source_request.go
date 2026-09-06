package runtime

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) planEvaluationV2DataSourceRequest(
	evaluation *planEvaluationV2,
	node *Node,
) (planStepV2Request, error) {
	if e == nil || e.DAG == nil {
		return planStepV2Request{}, fmt.Errorf("executor dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil ||
		evaluation.prior == nil || evaluation.configurations == nil {
		return planStepV2Request{}, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return planStepV2Request{}, fmt.Errorf("data-source node is required")
	}
	if node.Kind != NodeDataSource || node.IsComposite() {
		return planStepV2Request{}, fmt.Errorf("%s: node must be a primitive data source", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeDataSource); err != nil {
		return planStepV2Request{}, err
	}
	if node.ForEach != nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: data-source instances must be expanded first", node.Address,
		)
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: library %q is not imported", node.Address, node.Alias,
		)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: data-source binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return planStepV2Request{}, fmt.Errorf(
			"%s: data-source library path does not match import", node.Address,
		)
	}
	registration, err := e.dataRegistration(node)
	if err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: %w", node.Address, err)
	}
	if registration == nil {
		return planStepV2Request{}, fmt.Errorf("%s: data-source registration is required", node.Address)
	}
	configuration, err := e.factoryConfigurationV2(library)
	if err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: %w", node.Address, err)
	}
	request := dataSourcePlanningRequest{Address: node.Address}
	if entry := evaluation.prior.Find(node.Address); entry != nil {
		if entry.Kind != state.StateDataSource || entry.Payload.DataSource == nil {
			return planStepV2Request{}, fmt.Errorf(
				"%s: prior state must be a primitive data source", node.Address,
			)
		}
		prior := cloneDataSourceStatePayload(*entry.Payload.DataSource)
		request.Prior = &prior
	}
	dependencies, err := e.planEvaluationV2NodeDependencies(evaluation, node)
	if err != nil {
		return planStepV2Request{}, err
	}
	request.DependsOn = slices.Clone(dependencies)
	planningNode := *node
	return planStepV2Request{
		Address: node.Address, Kind: NodeDataSource, DependsOn: dependencies,
		Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
			if ctx == nil {
				return nil, fmt.Errorf("data-source planning context is required")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if pass == nil || pass.facts == nil || pass.reads == nil {
				return nil, fmt.Errorf("planning pass state is required")
			}
			desired, err := e.planEvaluationV2DataSourceTarget(evaluation, &planningNode)
			if err != nil {
				return nil, err
			}
			planned := request
			planned.Desired = desired
			return e.planEvaluationV2DataSourceStep(ctx, evaluation, pass, planned,
				dataSourcePlanningCallbacks{Read: func(ctx context.Context) (EncodedValue, error) {
					return readPlanEvaluationV2DataSource(ctx, registration, configuration, *desired)
				}},
			)
		},
	}, nil
}

func readPlanEvaluationV2DataSource(
	ctx context.Context,
	registration DataSourceRegistration,
	configuration resolvedConfigurationDefinition,
	target PlannedDataSourceTarget,
) (EncodedValue, error) {
	receiver := registration.NewReceiver()
	value := reflect.ValueOf(receiver)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() ||
		value.Type().Elem().Kind() != reflect.Struct {
		return EncodedValue{}, fmt.Errorf("receiver must be a non-nil pointer to a struct")
	}
	root := value.Type().Elem()
	if err := validateResourceValueRoot(root, false); err != nil {
		return EncodedValue{}, err
	}
	inputs, err := resolveEncodedAssets(configuration.assetCache, target.Inputs)
	if err != nil {
		return EncodedValue{}, err
	}
	decoded, _, err := decodeResourceObject(root, inputs, false, "")
	if err != nil {
		return EncodedValue{}, fmt.Errorf("data-source inputs: %w", err)
	}
	if err := Decode(receiver, decoded.(map[string]any)); err != nil {
		return EncodedValue{}, fmt.Errorf("decode data-source inputs: %w", err)
	}
	_, config, err := configuration.prepareConfigurationRecord(*target.Configuration.Record)
	if err != nil {
		return EncodedValue{}, err
	}
	outputType := registration.OutputType()
	if outputType == nil {
		return EncodedValue{}, fmt.Errorf("data-source output type is required")
	}
	if err := validateResourceValueRoot(outputType, true); err != nil {
		return EncodedValue{}, err
	}
	outputs, err := registration.Read(ctx, receiver, config)
	if err != nil {
		return EncodedValue{}, err
	}
	output := reflect.ValueOf(outputs)
	if !output.IsValid() || output.Type() != outputType || output.IsNil() {
		return EncodedValue{}, fmt.Errorf("data-source outputs must be a non-nil %s", outputType)
	}
	return encodeResourceValue(outputType.Elem(), output.Elem().Interface(), "")
}
