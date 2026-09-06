package runtime

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) planFactoryStepsV2(
	ctx context.Context,
	inputs EncodedValue,
	snapshot *state.SnapshotV2,
) ([]PlanStepV2, error) {
	if e == nil || e.LibraryCatalog == nil {
		return nil, fmt.Errorf("factory library catalog is required")
	}
	return runFixedPointPlanning(ctx, func(pass *planningPassState) ([]PlanStepV2, error) {
		evaluation, err := e.preparePlanEvaluationV2(inputs, snapshot, pass)
		if err != nil {
			return nil, err
		}
		steps := make([]PlanStepV2, 0, len(e.DAG.Nodes))
		requests := make([]planStepV2Request, 0, len(e.DAG.Nodes))
		live := make(map[string]bool, len(e.DAG.Nodes))
		order := evaluation.run.order
		if e.Destroy {
			order = nil
		}
		for _, address := range order {
			addresses, err := e.expandPlanEvaluationV2Dependency(evaluation, address)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", address, err)
			}
			for _, instance := range addresses {
				node := *e.DAG.Nodes[address]
				node.Address, node.ForEach = instance, nil
				request, err := e.planFactoryNodeV2(evaluation, &node)
				if err != nil {
					return nil, err
				}
				step, err := request.Plan(ctx, pass)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", instance, err)
				}
				if err := validatePlannedStepV2(request, step); err != nil {
					return nil, err
				}
				steps = append(steps, *step)
				requests = append(requests, request)
				live[instance] = true
			}
		}
		for _, entry := range evaluation.prior.Entries {
			if live[entry.Address] {
				continue
			}
			step, err := e.planRemovedFactoryEntryV2(ctx, pass, entry)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", entry.Address, err)
			}
			steps = append(steps, *step)
			requests = append(requests, planStepV2Request{
				Address: step.Address, Kind: step.Kind, DependsOn: step.DependsOn,
				Plan: func(context.Context, *planningPassState) (*PlanStepV2, error) { return step, nil },
			})
		}
		if _, err := preparePlanStepV2Requests(requests); err != nil {
			return nil, err
		}
		return steps, nil
	})
}

func (e *Executor) planRemovedFactoryEntryV2(
	ctx context.Context,
	pass *planningPassState,
	entry state.StateEntryV2,
) (*PlanStepV2, error) {
	dependencies := stateEntryV2Dependencies(&entry)
	switch entry.Kind {
	case state.StateResource:
		prior := &entry.Payload.Resource.Target
		registration, configuration, err := e.factoryResourceV2(prior.Binding)
		if err != nil {
			return nil, err
		}
		return planRegisteredResourceStep(ctx, pass, registeredResourcePlanningRequest{
			Address: entry.Address, DependsOn: dependencies, Prior: prior,
			PriorRegistration: registration, PriorConfigType: configuration,
		})
	case state.StateDataSource:
		return planDataSourceStep(ctx, dataSourcePlanningRequest{
			Address: entry.Address, DependsOn: dependencies, Prior: entry.Payload.DataSource,
		}, dataSourcePlanningCallbacks{})
	case state.StateAction:
		return planActionStep(actionPlanningRequest{
			Address: entry.Address, DependsOn: dependencies, Prior: entry.Payload.Action,
		})
	case state.StateComposite:
		return planCompositeStep(compositePlanningRequest{
			Address: entry.Address, DependsOn: dependencies, Prior: entry.Payload.Composite,
			Category: NodeKind(entry.Payload.Composite.Category),
		})
	default:
		return nil, fmt.Errorf("unsupported recorded entry kind %q", entry.Kind)
	}
}

func (e *Executor) planFactoryNodeV2(
	evaluation *planEvaluationV2,
	node *Node,
) (planStepV2Request, error) {
	if node.IsComposite() {
		return e.planFactoryCompositeRequestV2(evaluation, node)
	}
	switch node.Kind {
	case NodeResource:
		return e.planEvaluationV2ResourceRequest(evaluation, node, nil)
	case NodeDataSource:
		return e.planEvaluationV2DataSourceRequest(evaluation, node)
	case NodeAction:
		return e.planEvaluationV2ActionRequest(evaluation, node)
	case NodeLibraryConfig:
		return e.planEvaluationV2LibraryConfigurationRequest(evaluation, node)
	case NodeOutput:
		return e.planEvaluationV2OutputRequest(evaluation, node)
	default:
		return planStepV2Request{}, fmt.Errorf("%s: unsupported node kind %q", node.Address, node.Kind)
	}
}

func (e *Executor) planFactoryCompositeRequestV2(
	evaluation *planEvaluationV2,
	node *Node,
) (planStepV2Request, error) {
	dependencies, err := e.planEvaluationV2NodeDependencies(evaluation, node)
	if err != nil {
		return planStepV2Request{}, err
	}
	request := compositePlanningRequest{
		Address: node.Address, Category: node.Kind, DependsOn: dependencies,
	}
	if entry := evaluation.prior.Find(node.Address); entry != nil {
		if entry.Kind != state.StateComposite || entry.Payload.Composite == nil {
			return planStepV2Request{}, fmt.Errorf("%s: prior state must be a composite", node.Address)
		}
		request.Prior = entry.Payload.Composite
	}
	return planStepV2Request{
		Address: node.Address, Kind: node.Kind, DependsOn: dependencies,
		Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
			desired, err := e.planEvaluationV2CompositeTarget(evaluation, node)
			if err != nil {
				return nil, err
			}
			planned := request
			planned.Desired = desired
			return e.planEvaluationV2CompositeStep(ctx, evaluation, planned)
		},
	}, nil
}
