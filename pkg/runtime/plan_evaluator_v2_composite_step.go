package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) planEvaluationV2CompositeStep(
	ctx context.Context,
	evaluation *planEvaluationV2,
	request compositePlanningRequest,
) (*PlanStepV2, error) {
	if ctx == nil {
		return nil, fmt.Errorf("composite planning context is required")
	}
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil {
		return nil, fmt.Errorf("version 2 plan evaluation is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	step, err := planCompositeStep(request)
	if err != nil || step == nil {
		return nil, err
	}
	var outputs *EncodedValue
	if desired := step.Operation.Composite.Desired; desired != nil {
		node := e.DAG.Nodes[templateAddress(step.Address)]
		if node == nil || !node.IsComposite() || node.Kind != step.Kind {
			return nil, fmt.Errorf("%s: matching composite node is required", step.Address)
		}
		library := e.librariesFor(node)[node.Alias]
		if library == nil || desired.Binding != (Binding{
			LibraryPath: library.LibraryPath, Export: node.Type,
		}) {
			return nil, fmt.Errorf("%s: composite binding does not match node", step.Address)
		}
		if !desired.Inputs.HasPending() {
			scope, err := e.ensureCompositeScope(evaluation.run, step.Address)
			if err != nil {
				return nil, err
			}
			outputScope := *scope
			fields, _ := desired.Inputs.ObjectFields()
			outputScope.Inputs, err = decodeConcreteObjectFields(fields, "composite input")
			if err != nil {
				return nil, err
			}
			outputScope.locals = compositeLocalScope(node)
			values, err := planCompositeOutputs(node, &outputScope)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", step.Address, err)
			}
			encoded := make(map[string]EncodedValue, len(values))
			for name, value := range values {
				field, err := encodePlanningValue(typecheck.TUnknown(), value)
				if err != nil {
					return nil, fmt.Errorf("%s: composite output %q: %w", step.Address, name, err)
				}
				if !field.HasPending() {
					encoded[name] = field
				}
			}
			result, err := ObjectValue(encoded)
			if err != nil {
				return nil, err
			}
			outputs = &result
		}
	}
	parent, err := e.scopeForAddress(evaluation.run, step.Address)
	if err != nil {
		return nil, err
	}
	if parent != nil {
		if err := publishPlanEvaluationV2Outputs(
			scopeMapForKind(parent, step.Kind), step.Address, outputs,
		); err != nil {
			return nil, err
		}
	}
	return step, nil
}
