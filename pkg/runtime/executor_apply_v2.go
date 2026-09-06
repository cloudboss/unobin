package runtime

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// ApplyPlanV2 executes the reviewed decisions against the recorded state revision.
func (e *Executor) ApplyPlanV2(
	ctx context.Context, plan *PlanFileV2,
) (_ *ExecResult, err error) {
	stage := ApplyFailureSetup
	defer func() {
		if err != nil {
			if _, ok := AsApplyFailure(err); !ok {
				err = NewApplyFailure(stage, err)
			}
		}
	}()
	if ctx == nil {
		return nil, fmt.Errorf("apply context is required")
	}
	if e == nil || e.DAG == nil {
		return nil, fmt.Errorf("executor dependency graph is required")
	}
	if e.LibraryCatalog == nil {
		return nil, fmt.Errorf("factory library catalog is required")
	}
	if plan == nil {
		return nil, fmt.Errorf("saved plan is required")
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("saved plan: %w", err)
	}
	apply := &factoryApplyV2{
		executor: e, plan: plan,
		configurations: map[string]planEvaluationV2Configuration{},
		dependencies:   persistedPlanDependenciesV2(plan.Steps),
	}
	result, err := applyPlanFileV2WithStateLock(ctx, e.Store, e.Factory, *plan,
		applyPlanStepsV2Callbacks{
			Schedule: applyScheduleV2Options{
				Nodes: e.DAG.Nodes, Drain: e.Drain, Events: e.Events, Parallelism: e.Parallelism,
			},
			Prepare: apply.prepare, Resource: apply.resource, DataSource: apply.dataSource,
			Action: apply.action, LibraryConfiguration: apply.configuration,
			Composite: apply.composite, Output: apply.output,
		},
	)
	if err != nil {
		return nil, err
	}
	stage = ApplyFailureFinalize
	evaluation, err := apply.evaluation(result.Snapshot)
	if err != nil {
		return nil, err
	}
	fields, _ := result.Snapshot.Outputs.ObjectFields()
	outputs, err := decodeConcreteObjectFields(fields, "factory output")
	if err != nil {
		return nil, err
	}
	return &ExecResult{
		Outputs: outputs, Actions: evaluation.run.eval.Actions, Data: evaluation.run.eval.Data,
		WrittenRev: result.WrittenRevision,
	}, nil
}

type factoryApplyV2 struct {
	dependencies   map[string][]string
	executor       *Executor
	plan           *PlanFileV2
	state          *applyStateV2
	mu             sync.Mutex
	configurations map[string]planEvaluationV2Configuration
}

func (a *factoryApplyV2) prepare(_ context.Context, applyState *applyStateV2) error {
	snapshot, err := applyState.snapshotCopy()
	if err != nil {
		return err
	}
	destroy := a.plan.Mode == PlanDestroy
	if err := a.executor.validateFactoryV2Bindings(snapshot, destroy); err != nil {
		return err
	}
	for _, step := range a.plan.Steps {
		if operation := step.Operation.Resource; operation != nil {
			for _, binding := range resourceOperationBindings(*operation) {
				if _, _, err := a.executor.factoryResourceV2(binding); err != nil {
					return fmt.Errorf("%s: %w", step.Address, err)
				}
			}
		}
	}
	a.state = applyState
	return nil
}

func resourceOperationBindings(operation ResourcePlanOperation) []Binding {
	var bindings []Binding
	if operation.Desired != nil {
		bindings = append(bindings, operation.Desired.Binding)
	}
	if operation.Prior != nil {
		bindings = append(bindings, operation.Prior.Binding)
	}
	return bindings
}

func (a *factoryApplyV2) evaluation(snapshot *state.SnapshotV2) (*planEvaluationV2, error) {
	fields, _ := a.plan.Inputs.ObjectFields()
	inputs, err := decodeConcreteObjectFields(fields, "saved input")
	if err != nil {
		return nil, err
	}
	run, err := a.executor.newEvaluationRunState(inputs)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	configurations := maps.Clone(a.configurations)
	a.mu.Unlock()
	evaluation := &planEvaluationV2{run: run, prior: snapshot, configurations: configurations}
	pass := &planningPassState{facts: &planningFacts{}}
	if err := a.executor.seedPlanEvaluationV2(evaluation, pass); err != nil {
		return nil, err
	}
	return evaluation, nil
}

func (a *factoryApplyV2) evaluateStep(
	applyState *applyStateV2, step PlanStepV2,
) (*planEvaluationV2, *Node, error) {
	snapshot, err := applyState.snapshotCopy()
	if err != nil {
		return nil, nil, err
	}
	evaluation, err := a.evaluation(snapshot)
	if err != nil {
		return nil, nil, err
	}
	if a.plan.Mode == PlanDestroy {
		return evaluation, nil, nil
	}
	source := a.executor.DAG.Nodes[templateAddress(step.Address)]
	if source == nil {
		if planStepV2Decision(step) == DecisionDestroy {
			return evaluation, nil, nil
		}
		return nil, nil, fmt.Errorf("%s: saved node is absent from the factory", step.Address)
	}
	addresses, err := a.executor.expandPlanEvaluationV2Dependency(evaluation, source.Address)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(addresses, step.Address) {
		if planStepV2Decision(step) == DecisionDestroy {
			return evaluation, nil, nil
		}
		return nil, nil, fmt.Errorf("%s: saved instance is absent from the factory", step.Address)
	}
	if planStepV2Decision(step) == DecisionDestroy {
		return nil, nil, fmt.Errorf(
			"%s: saved destroy requires the node to remain absent", step.Address,
		)
	}
	node := *source
	node.Address, node.ForEach = step.Address, nil
	kind := node.Kind
	if kind == NodeLibraryConfig {
		kind = NodeLibraryConfiguration
	}
	if kind != step.Kind || node.IsComposite() != (step.Operation.Kind == StepComposite) {
		return nil, nil, fmt.Errorf("%s: node category does not match the saved plan", step.Address)
	}
	dependencies, err := a.executor.planEvaluationV2NodeDependencies(evaluation, &node)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Equal(dependencies, step.DependsOn) {
		return nil, nil, fmt.Errorf("%s: dependencies do not match the saved plan", step.Address)
	}
	return evaluation, &node, nil
}

func (a *factoryApplyV2) resource(
	ctx context.Context, applyState *applyStateV2, step PlanStepV2,
) error {
	evaluation, node, err := a.evaluateStep(applyState, step)
	if err != nil {
		return err
	}
	request := registeredResourceSnapshotApplyRequest{Step: a.persistedStep(step)}
	if node != nil {
		library := a.executor.librariesFor(node)[node.Alias]
		binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
		request.DesiredRegistration, request.DesiredConfigType, err =
			a.executor.factoryResourceV2(binding)
		if err != nil {
			return err
		}
		request.Desired, err = a.executor.planEvaluationV2ResourceTarget(
			evaluation, node, request.DesiredRegistration,
		)
		if err != nil {
			return err
		}
	}
	if prior := step.Operation.Resource.Prior; prior != nil {
		request.PriorRegistration, request.PriorConfigType, err =
			a.executor.factoryResourceV2(prior.Binding)
		if err != nil {
			return err
		}
	}
	_, err = applyRegisteredResourceSnapshotStep(ctx, applyState, request)
	return err
}

func (a *factoryApplyV2) configuration(
	ctx context.Context, applyState *applyStateV2, step PlanStepV2,
) error {
	evaluation, node, err := a.evaluateStep(applyState, step)
	if err != nil {
		return err
	}
	library := a.executor.librariesFor(node)[node.Alias]
	definition, err := a.executor.factoryConfigurationV2(library)
	if err != nil {
		return err
	}
	scope, err := a.executor.enclosingScope(evaluation.run, node.Address)
	if err != nil {
		return err
	}
	values, err := evalConfigurationBody(node.Body, scope)
	if err != nil {
		return err
	}
	inputs, err := definition.encodePlanningValue(values)
	if err != nil {
		return err
	}
	sensitivePaths := a.executor.configurationSensitivePathsV2(node)
	operation := *step.Operation.LibraryConfiguration
	prior := operation.Result.Record
	if prior == nil {
		prior = evaluation.priorConfiguration(
			node.Address, library.LibraryPath, inputs, sensitivePaths,
		)
	}
	var decoded any
	record, err := applyLibraryConfigurationOperation(ctx,
		libraryConfigurationApplyRequest{
			Address: step.Address, Operation: operation, Inputs: inputs,
		},
		libraryConfigurationApplyCallbacks{
			Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				record, err := definition.newConfigurationRecord(
					node.Address, inputs, sensitivePaths, prior,
				)
				if err != nil {
					return ConfigurationRecord{}, err
				}
				record, decoded, err = definition.prepareConfigurationRecord(record)
				return record, err
			},
		},
	)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.configurations[step.Address] = planEvaluationV2Configuration{
		definition: definition, decoded: decoded,
		planned: PlannedConfiguration{Kind: PlannedConfigurationConcrete, Record: record},
	}
	a.mu.Unlock()
	return nil
}

func (a *factoryApplyV2) composite(
	ctx context.Context, applyState *applyStateV2, step PlanStepV2,
) error {
	evaluation, node, err := a.evaluateStep(applyState, step)
	if err != nil {
		return err
	}
	request := compositeSnapshotApplyRequest{Step: a.persistedStep(step)}
	if node != nil {
		request.Desired, err = a.executor.planEvaluationV2CompositeTarget(evaluation, node)
		if err != nil {
			return err
		}
		request.Eval = func(context.Context) (EncodedValue, error) {
			scope, err := a.executor.ensureCompositeScope(evaluation.run, node.Address)
			if err != nil {
				return EncodedValue{}, err
			}
			values, err := evalCompositeOutputs(node, scope)
			if err != nil {
				return EncodedValue{}, err
			}
			return encodeFactoryApplyV2Object(values)
		}
	}
	_, err = applyCompositeSnapshotStep(ctx, applyState, request)
	return err
}

func encodeFactoryApplyV2Object(values map[string]any) (EncodedValue, error) {
	fields := make(map[string]EncodedValue, len(values))
	for name, value := range values {
		encoded, err := encodePlanningValue(typecheck.TUnknown(), value)
		if err != nil {
			return EncodedValue{}, fmt.Errorf("field %q: %w", name, err)
		}
		fields[name] = encoded
	}
	return ObjectValue(fields)
}

func (a *factoryApplyV2) output(ctx context.Context, step PlanStepV2) (EncodedValue, error) {
	evaluation, node, err := a.evaluateStep(a.state, step)
	if err != nil {
		return EncodedValue{}, err
	}
	request, err := a.executor.planEvaluationV2OutputRequest(evaluation, node)
	if err != nil {
		return EncodedValue{}, err
	}
	current, err := request.Plan(ctx, nil)
	if err != nil {
		return EncodedValue{}, err
	}
	if current.Operation.Output.Sensitive != step.Operation.Output.Sensitive {
		return EncodedValue{}, fmt.Errorf("output sensitivity does not match the saved plan")
	}
	return current.Operation.Output.Value, nil
}

func (a *factoryApplyV2) persistedStep(step PlanStepV2) PlanStepV2 {
	step.DependsOn = slices.Clone(a.dependencies[step.Address])
	return step
}

// Configuration and output steps have no durable entry; retain their durable predecessors.
func persistedPlanDependenciesV2(steps []PlanStepV2) map[string][]string {
	byAddress := make(map[string]PlanStepV2, len(steps))
	for _, step := range steps {
		byAddress[step.Address] = step
	}
	result := make(map[string][]string, len(steps))
	for _, step := range steps {
		pending := slices.Clone(step.DependsOn)
		seen := map[string]bool{}
		dependencies := []string{}
		for len(pending) > 0 {
			address := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if seen[address] {
				continue
			}
			seen[address] = true
			dependency, ok := byAddress[address]
			if ok && (dependency.Operation.Kind == StepLibraryConfiguration ||
				dependency.Operation.Kind == StepOutput) {
				pending = append(pending, dependency.DependsOn...)
				continue
			}
			dependencies = append(dependencies, address)
		}
		slices.Sort(dependencies)
		result[step.Address] = dependencies
	}
	return result
}
