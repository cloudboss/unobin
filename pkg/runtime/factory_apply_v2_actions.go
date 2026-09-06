package runtime

import (
	"context"
	"fmt"
	"reflect"
)

func (a *factoryApplyV2) dataSource(
	ctx context.Context, applyState *applyStateV2, step PlanStepV2,
) error {
	evaluation, node, err := a.evaluateStep(applyState, step)
	if err != nil {
		return err
	}
	request := dataSourceSnapshotApplyRequest{Step: a.persistedStep(step)}
	if node != nil {
		request.Desired, err = a.executor.planEvaluationV2DataSourceTarget(evaluation, node)
		if err != nil {
			return err
		}
		registration, err := a.executor.dataRegistration(node)
		if err != nil {
			return err
		}
		library := a.executor.librariesFor(node)[node.Alias]
		definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
		if err != nil {
			return err
		}
		request.Read = func(ctx context.Context) (EncodedValue, error) {
			return readPlanEvaluationV2DataSource(ctx, registration, definition, *request.Desired)
		}
	}
	_, err = applyDataSourceSnapshotStep(ctx, applyState, request)
	return err
}

func (a *factoryApplyV2) action(
	ctx context.Context, applyState *applyStateV2, step PlanStepV2,
) error {
	evaluation, node, err := a.evaluateStep(applyState, step)
	if err != nil {
		return err
	}
	request := actionSnapshotApplyRequest{Step: a.persistedStep(step)}
	if node != nil {
		request.Desired, err = a.executor.planEvaluationV2ActionTarget(evaluation, node)
		if err != nil {
			return err
		}
		registration, err := a.executor.actionRegistration(node)
		if err != nil {
			return err
		}
		library := a.executor.librariesFor(node)[node.Alias]
		definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
		if err != nil {
			return err
		}
		request.Run = func(ctx context.Context) (EncodedValue, error) {
			return runFactoryV2Action(ctx, registration, definition, *request.Desired)
		}
	}
	_, err = applyActionSnapshotStep(ctx, applyState, request)
	return err
}

func runFactoryV2Action(
	ctx context.Context,
	registration ActionRegistration,
	configuration resolvedConfigurationDefinition,
	target PlannedActionTarget,
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
	decoded, _, err := decodeResourceObject(root, target.Inputs, false, "")
	if err != nil {
		return EncodedValue{}, fmt.Errorf("action inputs: %w", err)
	}
	if err := Decode(receiver, decoded.(map[string]any)); err != nil {
		return EncodedValue{}, fmt.Errorf("decode action inputs: %w", err)
	}
	_, config, err := configuration.prepareConfigurationRecord(*target.Configuration.Record)
	if err != nil {
		return EncodedValue{}, err
	}
	outputType := registration.OutputType()
	if outputType == nil {
		return EncodedValue{}, fmt.Errorf("action output type is required")
	}
	if err := validateResourceValueRoot(outputType, true); err != nil {
		return EncodedValue{}, err
	}
	outputs, err := registration.Run(ctx, receiver, config)
	if err != nil {
		return EncodedValue{}, err
	}
	output := reflect.ValueOf(outputs)
	if !output.IsValid() || output.Type() != outputType || output.IsNil() {
		return EncodedValue{}, fmt.Errorf("action outputs must be a non-nil %s", outputType)
	}
	return encodeResourceValue(outputType.Elem(), output.Elem().Interface(), "")
}
