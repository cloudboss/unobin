package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) planEvaluationV2CompositeTarget(
	evaluation *planEvaluationV2,
	node *Node,
) (*PlannedCompositeTarget, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return nil, fmt.Errorf("composite node is required")
	}
	if !validCompositeCategory(node.Kind) || !node.IsComposite() {
		return nil, fmt.Errorf("%s: node must be a composite", node.Address)
	}
	if err := validateNodeAddress(node.Address, node.Kind); err != nil {
		return nil, err
	}
	if node.ForEach != nil {
		return nil, fmt.Errorf("%s: composite instances must be expanded first", node.Address)
	}
	libraries := e.librariesFor(node)
	library := libraries[node.Alias]
	if library == nil {
		return nil, fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("%s: composite binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return nil, fmt.Errorf("%s: composite library path does not match import", node.Address)
	}
	if lookupComposite(libraries, node.Alias, node.Kind, node.Type) == nil {
		return nil, fmt.Errorf(
			"%s: library %q has no %s composite %q", node.Address, node.Alias, node.Kind, node.Type,
		)
	}
	scope, err := e.planEvaluationV2Scope(evaluation, node)
	if err != nil {
		return nil, err
	}
	values, _, err := planEvalBody(node.Body, scope)
	if err != nil {
		return nil, err
	}
	inputs, err := encodePlanEvaluationV2CompositeInputs(node, values)
	if err != nil {
		return nil, fmt.Errorf("%s: composite inputs: %w", node.Address, err)
	}
	if err := e.checkPlanEvaluationV2Constraints(node, inputs); err != nil {
		return nil, err
	}
	sensitivity := e.sensitivityAnalyzer()
	target := PlannedCompositeTarget{
		Category: node.Kind,
		Binding:  binding,
		Inputs:   inputs,
		SensitiveInputPaths: planEvaluationV2SensitivePaths(
			sensitivity.stepSensitiveInputs(node),
		),
		SensitiveOutputPaths: planEvaluationV2SensitivePaths(
			sensitivity.sensitiveOutputs(node),
		),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%s: desired composite target: %w", node.Address, err)
	}
	return &target, nil
}

func encodePlanEvaluationV2CompositeInputs(
	node *Node,
	values map[string]any,
) (EncodedValue, error) {
	declarations := node.CompositeSyntaxBody.Inputs
	fields := make([]typecheck.ObjectField, 0, len(declarations))
	for _, declaration := range declarations {
		typ := typecheck.FromLang(declaration.Type)
		if configuration, ok := declaration.Type.(*lang.TypeLibraryConfig); ok {
			if configuration.Path == nil {
				return EncodedValue{}, fmt.Errorf("library-config path is required")
			}
			schema, ok := node.LibraryConfigSchemas[configuration.Path.Value]
			if !ok {
				return EncodedValue{}, fmt.Errorf(
					"library-config %q has no resolved schema", configuration.Path.Value,
				)
			}
			typ = schema.TypecheckType()
		}
		optional := typ.Kind == typecheck.Optional
		if optional {
			typ = typ.Unwrap()
		}
		fields = append(fields, typecheck.ObjectField{
			Name: declaration.Name.Name, Type: typ, Optional: optional,
		})
	}
	return encodePlanningConfigurationObject(fields, values)
}
