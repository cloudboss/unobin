package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planEvaluationV2ResourceResolver func(Binding) (
	*resourceDefinitionRegistration,
	*resolvedConfigurationDefinition,
	error,
)

func (e *Executor) planEvaluationV2ResourceRequest(
	evaluation *planEvaluationV2,
	node *Node,
	resolve planEvaluationV2ResourceResolver,
) (planStepV2Request, error) {
	if e == nil || e.DAG == nil {
		return planStepV2Request{}, fmt.Errorf("executor dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.run.eval == nil ||
		evaluation.prior == nil || evaluation.configurations == nil {
		return planStepV2Request{}, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return planStepV2Request{}, fmt.Errorf("resource node is required")
	}
	if node.Kind != NodeResource || node.IsComposite() {
		return planStepV2Request{}, fmt.Errorf("%s: node must be a primitive resource", node.Address)
	}
	if err := validateNodeAddress(node.Address, NodeResource); err != nil {
		return planStepV2Request{}, err
	}
	if node.ForEach != nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: resource instances must be expanded first", node.Address,
		)
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: library %q is not imported", node.Address, node.Alias,
		)
	}
	binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
	if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
		return planStepV2Request{}, fmt.Errorf(
			"%s: resource library path does not match import", node.Address,
		)
	}
	if resolve == nil && e.LibraryCatalog != nil {
		resolve = e.LibraryCatalog.resource
	}
	registration, configuration, err := resolve.resource(binding)
	if err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: desired resource: %w", node.Address, err)
	}
	request := registeredResourcePlanningRequest{
		Address: node.Address, DesiredRegistration: registration, DesiredConfigType: configuration,
	}
	if entry := evaluation.prior.Find(node.Address); entry != nil {
		if entry.Kind != state.StateResource || entry.Payload.Resource == nil {
			return planStepV2Request{}, fmt.Errorf(
				"%s: prior state must be a primitive resource", node.Address,
			)
		}
		prior := cloneResourceTarget(entry.Payload.Resource.Target)
		request.Prior = &prior
		request.PriorRegistration, request.PriorConfigType = registration, configuration
		if prior.Binding != binding {
			request.PriorRegistration, request.PriorConfigType, err = resolve.resource(prior.Binding)
			if err != nil {
				return planStepV2Request{}, fmt.Errorf("%s: prior resource: %w", node.Address, err)
			}
		}
	}
	dependencies, err := e.planEvaluationV2NodeDependencies(evaluation, node)
	if err != nil {
		return planStepV2Request{}, err
	}
	request.DependsOn = slices.Clone(dependencies)
	planningNode := *node
	return planStepV2Request{
		Address: node.Address, Kind: NodeResource, DependsOn: dependencies,
		Plan: func(ctx context.Context, pass *planningPassState) (*PlanStepV2, error) {
			if ctx == nil {
				return nil, fmt.Errorf("resource planning context is required")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if pass == nil || pass.facts == nil {
				return nil, fmt.Errorf("planning pass state is required")
			}
			desired, err := e.planEvaluationV2ResourceTarget(evaluation, &planningNode, registration)
			if err != nil {
				return nil, err
			}
			planned := request
			planned.Desired = desired
			return e.planEvaluationV2ResourceStep(ctx, evaluation, pass, planned)
		},
	}, nil
}

func (resolve planEvaluationV2ResourceResolver) resource(binding Binding) (
	*resourceDefinitionRegistration,
	*resolvedConfigurationDefinition,
	error,
) {
	if err := binding.Validate(); err != nil {
		return nil, nil, err
	}
	if resolve == nil {
		return nil, nil, fmt.Errorf("resource resolver is required")
	}
	registration, configuration, err := resolve(binding)
	if err != nil {
		return nil, nil, err
	}
	if registration == nil {
		return nil, nil, fmt.Errorf("resource registration is required")
	}
	if configuration == nil {
		return nil, nil, fmt.Errorf("configuration definition is required")
	}
	if configuration.libraryPath != binding.LibraryPath {
		return nil, nil, fmt.Errorf("configuration definition does not match binding")
	}
	return registration, configuration, nil
}
