package runtime

import (
	"fmt"
	"reflect"
)

func (e *Executor) planEvaluationV2DataSourceTarget(
	evaluation *planEvaluationV2,
	node *Node,
) (*PlannedDataSourceTarget, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil ||
		evaluation.configurations == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return nil, fmt.Errorf("data-source node is required")
	}
	if node.Kind != NodeDataSource || node.IsComposite() {
		return nil, fmt.Errorf("%s: node must be a primitive data source", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeDataSource); err != nil {
		return nil, err
	}
	if node.ForEach != nil {
		return nil, fmt.Errorf("%s: data-source instances must be expanded first", node.Address)
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return nil, fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("%s: data-source binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return nil, fmt.Errorf("%s: data-source library path does not match import", node.Address)
	}
	registration, err := e.dataRegistration(node)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", node.Address, err)
	}
	if registration == nil {
		return nil, fmt.Errorf("%s: data-source registration is required", node.Address)
	}
	configuration, err := e.planEvaluationV2ConfigurationForNode(evaluation, node, library)
	if err != nil {
		return nil, err
	}
	scope, err := e.planEvaluationV2Scope(evaluation, node)
	if err != nil {
		return nil, err
	}
	values, pending, err := planEvalBody(node.Body, scope)
	if err != nil {
		return nil, err
	}
	values = cloneConfigMap(values)
	defaults := library.Defaults["data-source."+node.Type]
	if err := overlayDefaults(values, defaults, pending); err != nil {
		return nil, err
	}
	inputs, err := guard("preparing data-source inputs", false, func() (EncodedValue, error) {
		receiver := reflect.ValueOf(registration.NewReceiver())
		if !receiver.IsValid() || receiver.Kind() != reflect.Pointer || receiver.IsNil() ||
			receiver.Type().Elem().Kind() != reflect.Struct {
			return EncodedValue{}, fmt.Errorf("receiver must be a non-nil pointer to a struct")
		}
		root := receiver.Type().Elem()
		if err := validateResourceValueRoot(root, false); err != nil {
			return EncodedValue{}, err
		}
		return encodeResourceObject(root, values, "")
	})
	if err != nil {
		return nil, fmt.Errorf("%s: data-source inputs: %w", node.Address, err)
	}
	sensitivity := e.sensitivityAnalyzer()
	target := PlannedDataSourceTarget{
		Binding:       binding,
		Inputs:        inputs,
		Configuration: configuration,
		SensitiveInputPaths: planEvaluationV2SensitivePaths(
			sensitivity.stepSensitiveInputs(node),
		),
		SensitiveOutputPaths: planEvaluationV2SensitivePaths(
			sensitivity.sensitiveOutputs(node),
		),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%s: desired data-source target: %w", node.Address, err)
	}
	return &target, nil
}
