package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planEvaluationV2 struct {
	run            *runState
	prior          *state.SnapshotV2
	configurations map[string]planEvaluationV2Configuration
}

type planEvaluationV2Configuration struct {
	definition resolvedConfigurationDefinition
	planned    PlannedConfiguration
	decoded    any
}

func (e *Executor) preparePlanEvaluationV2(
	inputs EncodedValue,
	snapshot *state.SnapshotV2,
	pass *planningPassState,
) (*planEvaluationV2, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}
	if snapshot == nil {
		return nil, fmt.Errorf("version 2 snapshot is required")
	}
	if pass == nil || pass.facts == nil {
		return nil, fmt.Errorf("planning pass state is required")
	}
	if err := validatePlanObject(inputs, "planning inputs", false); err != nil {
		return nil, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("version 2 snapshot: %w", err)
	}
	if e.LibraryCatalog != nil {
		if err := e.validatePlanEvaluationV2ResourceBindings(snapshot); err != nil {
			return nil, err
		}
	}

	prior, err := snapshot.Clone()
	if err != nil {
		return nil, fmt.Errorf("copy version 2 snapshot: %w", err)
	}
	fields, _ := inputs.ObjectFields()
	decodedInputs, err := decodeConcreteObjectFields(fields, "planning input")
	if err != nil {
		return nil, err
	}
	run, err := e.newEvaluationRunState(decodedInputs)
	if err != nil {
		return nil, err
	}
	evaluation := &planEvaluationV2{
		run:            run,
		prior:          prior,
		configurations: map[string]planEvaluationV2Configuration{},
	}
	if err := e.seedPlanEvaluationV2(evaluation, pass); err != nil {
		return nil, err
	}
	return evaluation, nil
}

func (e *Executor) planEvaluationV2LibraryConfigurationRequest(
	evaluation *planEvaluationV2,
	node *Node,
) (planStepV2Request, error) {
	if e == nil {
		return planStepV2Request{}, fmt.Errorf("executor is required")
	}
	if e.DAG == nil {
		return planStepV2Request{}, fmt.Errorf("dependency graph is required")
	}
	if evaluation == nil || evaluation.run == nil || evaluation.prior == nil ||
		evaluation.configurations == nil {
		return planStepV2Request{}, fmt.Errorf("version 2 plan evaluation is required")
	}
	if node == nil {
		return planStepV2Request{}, fmt.Errorf("library-configuration node is required")
	}
	if node.Kind != NodeLibraryConfig {
		return planStepV2Request{}, fmt.Errorf(
			"%s: node kind %q is not a library configuration",
			node.Address,
			node.Kind,
		)
	}
	if err := validateNodeAddress(node.Address, NodeLibraryConfiguration); err != nil {
		return planStepV2Request{}, err
	}
	library := e.librariesFor(node)[node.Alias]
	if library == nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: library %q is not imported",
			node.Address,
			node.Alias,
		)
	}
	if library.Configuration == nil {
		return planStepV2Request{}, fmt.Errorf(
			"%s: library %q declares no configuration",
			node.Address,
			node.Alias,
		)
	}
	definition, err := resolveLibraryConfigurationDefinition(library.LibraryPath, library)
	if err != nil {
		return planStepV2Request{}, fmt.Errorf("%s: %w", node.Address, err)
	}
	dependencies, err := e.planEvaluationV2NodeDependencies(evaluation, node)
	if err != nil {
		return planStepV2Request{}, err
	}
	sensitivePaths := planEvaluationV2SensitivePaths(
		e.sensitivityAnalyzer().sensitiveInputs(node.Body, node.Composite),
	)
	planningDependencies := slices.Clone(dependencies)
	planningSensitivePaths := slices.Clone(sensitivePaths)

	return planStepV2Request{
		Address:   node.Address,
		Kind:      NodeLibraryConfiguration,
		DependsOn: dependencies,
		Plan: func(ctx context.Context, _ *planningPassState) (*PlanStepV2, error) {
			return e.planEvaluationV2LibraryConfiguration(
				ctx,
				evaluation,
				node,
				planningDependencies,
				planningSensitivePaths,
				definition,
			)
		},
	}, nil
}

func planEvaluationV2Dependencies(edges []string) []string {
	dependencies := make([]string, 0, len(edges))
	for _, edge := range edges {
		if validAnyPlanAddress(edge) {
			dependencies = append(dependencies, edge)
		}
	}
	slices.Sort(dependencies)
	return dependencies
}

func (e *Executor) planEvaluationV2LibraryConfiguration(
	ctx context.Context,
	evaluation *planEvaluationV2,
	node *Node,
	dependencies []string,
	sensitivePaths []string,
	definition resolvedConfigurationDefinition,
) (*PlanStepV2, error) {
	scope, err := e.enclosingScope(evaluation.run, node.Address)
	if err != nil {
		return nil, err
	}
	values, _, err := planEvalBody(node.Body, scope)
	if err != nil {
		return nil, err
	}
	inputs, err := definition.encodePlanningValue(values)
	if err != nil {
		return nil, err
	}
	prior := evaluation.priorConfiguration(
		node.Address,
		definition.libraryPath,
		inputs,
		sensitivePaths,
	)
	var decoded any
	step, err := planLibraryConfigurationStep(
		ctx,
		libraryConfigurationPlanningRequest{
			Address:   node.Address,
			DependsOn: dependencies,
			Inputs:    inputs,
		},
		libraryConfigurationPlanningCallbacks{
			Eval: func(context.Context, EncodedValue) (ConfigurationRecord, error) {
				record, err := definition.newConfigurationRecord(
					node.Address,
					inputs,
					sensitivePaths,
					prior,
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
		return nil, err
	}
	planned := clonePlanEvaluationV2Configuration(
		step.Operation.LibraryConfiguration.Result,
	)
	evaluation.configurations[node.Address] = planEvaluationV2Configuration{
		definition: definition,
		planned:    planned,
		decoded:    decoded,
	}
	return step, nil
}

func (e *planEvaluationV2) priorConfiguration(
	address string,
	libraryPath string,
	value EncodedValue,
	sensitivePaths []string,
) *ConfigurationRecord {
	var fallback *ConfigurationRecord
	for i := range e.prior.Entries {
		record := planEvaluationV2EntryConfiguration(e.prior.Entries[i])
		if record == nil || record.Address != address || record.LibraryPath != libraryPath {
			continue
		}
		cloned := internalconfig.Clone(*record)
		if fallback == nil {
			fallback = &cloned
		}
		if encodedValuesEqual(record.Value, value) &&
			slices.Equal(record.SensitivePaths, sensitivePaths) {
			return &cloned
		}
	}
	return fallback
}

func planEvaluationV2EntryConfiguration(
	entry state.StateEntryV2,
) *ConfigurationRecord {
	switch entry.Kind {
	case state.StateResource:
		return &entry.Payload.Resource.Target.Configuration
	case state.StateAction:
		return &entry.Payload.Action.Configuration
	case state.StateDataSource:
		return &entry.Payload.DataSource.Configuration
	default:
		return nil
	}
}

func planEvaluationV2SensitivePaths(names []string) []string {
	paths := make([]string, len(names))
	for i, name := range names {
		name = strings.ReplaceAll(name, "~", "~0")
		name = strings.ReplaceAll(name, "/", "~1")
		paths[i] = "/" + name
	}
	return paths
}

func clonePlanEvaluationV2Configuration(
	planned PlannedConfiguration,
) PlannedConfiguration {
	planned.PendingRefs = slices.Clone(planned.PendingRefs)
	if planned.Record != nil {
		record := internalconfig.Clone(*planned.Record)
		planned.Record = &record
	}
	return planned
}

func (e *Executor) seedPlanEvaluationV2(
	evaluation *planEvaluationV2,
	pass *planningPassState,
) error {
	for i := range evaluation.prior.Entries {
		entry := evaluation.prior.Entries[i]
		if entry.Kind == state.StateResource && pass.outputsInvalidated(entry.Address) {
			continue
		}
		kind, outputs, err := planEvaluationV2EntryOutputs(entry)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Address, err)
		}
		fields, _ := outputs.ObjectFields()
		decoded, err := decodeConcreteObjectFields(fields, "state output")
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Address, err)
		}
		scope, err := e.scopeForAddress(evaluation.run, entry.Address)
		if err != nil {
			return fmt.Errorf("%s: prepare evaluation scope: %w", entry.Address, err)
		}
		if scope == nil {
			continue
		}
		target := scopeMapForKind(scope, kind)
		template, key := splitInstanceAddress(entry.Address)
		if key == "" {
			seedAddress(target, template, decoded)
		} else {
			seedAddressInstance(target, template, key, decoded)
		}
	}
	return nil
}

func planEvaluationV2EntryOutputs(
	entry state.StateEntryV2,
) (NodeKind, EncodedValue, error) {
	switch entry.Kind {
	case state.StateResource:
		if entry.Payload.Resource == nil {
			return "", EncodedValue{}, fmt.Errorf("resource payload is required")
		}
		return NodeResource, entry.Payload.Resource.Target.Outputs, nil
	case state.StateAction:
		if entry.Payload.Action == nil {
			return "", EncodedValue{}, fmt.Errorf("action payload is required")
		}
		return NodeAction, entry.Payload.Action.Outputs, nil
	case state.StateDataSource:
		if entry.Payload.DataSource == nil {
			return "", EncodedValue{}, fmt.Errorf("data-source payload is required")
		}
		return NodeDataSource, entry.Payload.DataSource.Outputs, nil
	case state.StateComposite:
		if entry.Payload.Composite == nil {
			return "", EncodedValue{}, fmt.Errorf("composite payload is required")
		}
		return NodeKind(entry.Payload.Composite.Category), entry.Payload.Composite.Outputs, nil
	default:
		return "", EncodedValue{}, fmt.Errorf("unknown state entry kind %q", entry.Kind)
	}
}
