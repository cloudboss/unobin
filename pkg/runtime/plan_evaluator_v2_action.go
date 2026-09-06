package runtime

import (
	"fmt"
	"reflect"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) planEvaluationV2ActionTarget(
	evaluation *planEvaluationV2,
	node *Node,
) (*PlannedActionTarget, error) {
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
		return nil, fmt.Errorf("action node is required")
	}
	if node.Kind != NodeAction || node.IsComposite() {
		return nil, fmt.Errorf("%s: node must be a primitive action", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeAction); err != nil {
		return nil, err
	}
	if node.ForEach != nil {
		return nil, fmt.Errorf("%s: action instances must be expanded first", node.Address)
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return nil, fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("%s: action binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return nil, fmt.Errorf("%s: action library path does not match import", node.Address)
	}
	registration, err := e.actionRegistration(node)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", node.Address, err)
	}
	if registration == nil {
		return nil, fmt.Errorf("%s: action registration is required", node.Address)
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
	defaults := library.Defaults["action."+node.Type]
	if err := overlayDefaults(values, defaults, pending); err != nil {
		return nil, err
	}
	inputs, err := guard("preparing action inputs", false, func() (EncodedValue, error) {
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
		return nil, fmt.Errorf("%s: action inputs: %w", node.Address, err)
	}
	if err := e.checkPlanEvaluationV2Constraints(node, inputs); err != nil {
		return nil, err
	}
	trigger, err := planEvaluationV2ActionTrigger(node, binding, inputs, scope)
	if err != nil {
		return nil, fmt.Errorf("%s: action trigger: %w", node.Address, err)
	}
	sensitivity := e.sensitivityAnalyzer()
	target := PlannedActionTarget{
		Binding:       binding,
		Inputs:        inputs,
		Configuration: configuration,
		TriggerHash:   trigger,
		SensitiveInputPaths: planEvaluationV2SensitivePaths(
			sensitivity.stepSensitiveInputs(node),
		),
		SensitiveOutputPaths: planEvaluationV2SensitivePaths(
			sensitivity.sensitiveOutputs(node),
		),
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%s: desired action target: %w", node.Address, err)
	}
	return &target, nil
}

func planEvaluationV2ActionTrigger(
	node *Node,
	binding Binding,
	inputs EncodedValue,
	scope *EvalContext,
) (string, error) {
	value := inputs
	body, ok := node.Body.(*lang.ObjectLit)
	if !ok {
		return "", fmt.Errorf("body must be an object literal")
	}
	for _, field := range body.Fields {
		if !field.Key.IsMeta() || field.Key.Name != "@trigger" {
			continue
		}
		var locals map[string]lang.Expr
		if scope.locals != nil {
			locals = scope.locals.exprs
		}
		evaluator := partialEvaluator{
			ec: scope, locals: locals, expanding: map[string]bool{},
		}
		evaluated, _, err := evaluator.element(field.Value)
		if err != nil {
			return "", err
		}
		if literal, ok := evaluated.(string); ok && literal == TriggerAlways {
			return "", nil
		}
		value, err = encodePlanningValue(typecheck.TUnknown(), evaluated)
		if err != nil {
			return "", err
		}
		break
	}
	if value.HasPending() {
		return "", nil
	}
	return hashJSON(map[string]any{"binding": binding, "value": value})
}
