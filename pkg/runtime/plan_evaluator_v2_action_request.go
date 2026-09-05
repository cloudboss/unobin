package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) planEvaluationV2ActionRequest(
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
		return planStepV2Request{}, fmt.Errorf("action node is required")
	}
	if node.Kind != NodeAction || node.IsComposite() {
		return planStepV2Request{}, fmt.Errorf("%s: node must be a primitive action", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeAction); err != nil {
		return planStepV2Request{}, err
	}
	if node.ForEach != nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: action instances must be expanded first", node.Address,
		)
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return planStepV2Request{}, fmt.Errorf("%s: library %q is not imported", node.Address, node.Alias)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if err := binding.Validate(); err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: action binding: %w", node.Address, err)
	}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return planStepV2Request{}, fmt.Errorf(
			"%s: action library path does not match import", node.Address,
		)
	}
	registration, err := e.actionRegistration(node)
	if err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: %w", node.Address, err)
	}
	if registration == nil {
		return planStepV2Request{}, fmt.Errorf("%s: action registration is required", node.Address)
	}
	request := actionPlanningRequest{Address: node.Address}
	if entry := evaluation.prior.Find(node.Address); entry != nil {
		if entry.Kind != state.StateAction || entry.Payload.Action == nil {
			return planStepV2Request{}, fmt.Errorf(
				"%s: prior state must be a primitive action", node.Address,
			)
		}
		prior := cloneActionStatePayload(*entry.Payload.Action)
		request.Prior = &prior
	}
	dependencies, err := e.planEvaluationV2NodeDependencies(evaluation, node)
	if err != nil {
		return planStepV2Request{}, err
	}
	request.DependsOn = slices.Clone(dependencies)
	planningNode := *node
	return planStepV2Request{
		Address: node.Address, Kind: NodeAction, DependsOn: dependencies,
		Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
			if ctx == nil {
				return nil, fmt.Errorf("action planning context is required")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			desired, err := e.planEvaluationV2ActionTarget(evaluation, &planningNode)
			if err != nil {
				return nil, err
			}
			planned := request
			planned.Desired = desired
			return e.planEvaluationV2ActionStep(ctx, evaluation, planned)
		},
	}, nil
}
