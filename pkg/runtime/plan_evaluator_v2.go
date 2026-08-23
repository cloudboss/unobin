package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type planEvaluationV2 struct {
	run   *runState
	prior *state.SnapshotV2
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
	evaluation := &planEvaluationV2{run: run, prior: prior}
	if err := e.seedPlanEvaluationV2(evaluation, pass); err != nil {
		return nil, err
	}
	return evaluation, nil
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
