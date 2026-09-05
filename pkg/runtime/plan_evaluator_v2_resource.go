package runtime

import "fmt"

func (e *Executor) planEvaluationV2ResourceTarget(
	evaluation *planEvaluationV2,
	node *Node,
	registration *resourceDefinitionRegistration,
) (*PlannedResourceTarget, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.configurations == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return nil, fmt.Errorf("resource node is required")
	}
	if node.Kind != NodeResource || node.IsComposite() {
		return nil, fmt.Errorf("%s: node must be a primitive resource", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeResource); err != nil {
		return nil, err
	}
	if node.ForEach != nil {
		return nil, fmt.Errorf("%s: resource instances must be expanded first", node.Address)
	}
	if registration == nil {
		return nil, fmt.Errorf("resource registration is required")
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return nil, fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("%s: resource binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return nil, fmt.Errorf("%s: resource library path does not match import", node.Address)
	}
	configuration, err := e.planEvaluationV2ResourceConfiguration(evaluation, node, library)
	if err != nil {
		return nil, err
	}
	scope, err := e.scopeFor(evaluation.run, node)
	if err != nil {
		return nil, err
	}
	values, pending, err := planEvalBody(node.Body, scope)
	if err != nil {
		return nil, err
	}
	values = cloneConfigMap(values)
	if err := overlayDefaults(values, library.Defaults["resource."+node.Type], pending); err != nil {
		return nil, err
	}
	inputs, err := registration.prepareInputs(values)
	if err != nil {
		return nil, fmt.Errorf("%s: resource inputs: %w", node.Address, err)
	}
	sensitivity := e.sensitivityAnalyzer()
	target := PlannedResourceTarget{
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
		return nil, fmt.Errorf("%s: desired resource target: %w", node.Address, err)
	}
	return &target, nil
}

func (e *Executor) planEvaluationV2ResourceConfiguration(
	evaluation *planEvaluationV2,
	node *Node,
	library *Library,
) (PlannedConfiguration, error) {
	if address, ok := libraryConfigNode(e.DAG.Nodes, node.Composite, node.Alias); ok {
		configuration, ok := evaluation.configurations[address]
		if !ok {
			return PlannedConfiguration{}, fmt.Errorf(
				"%s: library configuration %q has not been evaluated", node.Address, address,
			)
		}
		if configuration.definition.libraryPath != library.LibraryPath {
			return PlannedConfiguration{}, fmt.Errorf(
				"%s: library configuration %q does not match import", node.Address, address,
			)
		}
		return clonePlanEvaluationV2Configuration(configuration.planned), nil
	}
	if library.Configuration != nil && !library.Configuration.Empty() {
		return PlannedConfiguration{}, fmt.Errorf(
			"%s: library %q requires a configuration", node.Address, node.Alias,
		)
	}
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	if err != nil {
		return PlannedConfiguration{}, err
	}
	address := ""
	if !definition.noConfig {
		address = libraryConfigNodeAddress(node.Composite, node.Alias)
	}
	value, err := definition.encodePlanningValue(nil)
	if err != nil {
		return PlannedConfiguration{}, err
	}
	record, err := definition.newConfigurationRecord(address, value, nil, nil)
	if err != nil {
		return PlannedConfiguration{}, err
	}
	return PlannedConfiguration{Kind: PlannedConfigurationConcrete, Record: &record}, nil
}
