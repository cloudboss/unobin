package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func operationObject(t *testing.T, fields map[string]EncodedValue) EncodedValue {
	t.Helper()
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	return value
}

func concreteOperationConfiguration(t *testing.T) PlannedConfiguration {
	t.Helper()
	record := validConfigurationRecord(t)
	return PlannedConfiguration{
		Kind:   PlannedConfigurationConcrete,
		Record: &record,
	}
}

func pendingOperationConfiguration() PlannedConfiguration {
	return PlannedConfiguration{
		Kind:        PlannedConfigurationPending,
		PendingRefs: []string{"resource.network.id"},
	}
}

func validOperationBinding(export string) Binding {
	return Binding{LibraryPath: "example.com/cloud", Export: export}
}

func validOperationIdentity() IdentityRecord {
	stableID := "server-1"
	return IdentityRecord{
		DefinitionDigest: strings.Repeat("a", 64),
		Version:          1,
		StableID:         &stableID,
	}
}

func validOperationResourceTarget(t *testing.T) ResourceTarget {
	t.Helper()
	return ResourceTarget{
		Binding:       validOperationBinding("server"),
		SchemaVersion: 1,
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("api"),
			"size": IntegerValue(2),
		}),
		Outputs: operationObject(t, map[string]EncodedValue{
			"id": StringValue("server-1"),
		}),
		Configuration:        validConfigurationRecord(t),
		Identity:             validOperationIdentity(),
		DependsOn:            []string{"resource.network"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func validPlannedResourceTarget(t *testing.T) PlannedResourceTarget {
	t.Helper()
	return PlannedResourceTarget{
		Binding: validOperationBinding("server"),
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("api"),
			"size": IntegerValue(3),
		}),
		Configuration:        concreteOperationConfiguration(t),
		SensitiveInputPaths:  []string{"/name"},
		SensitiveOutputPaths: []string{"/id"},
	}
}

func presentOperationObservation(t *testing.T) ResourceObservation {
	t.Helper()
	outputs := operationObject(t, map[string]EncodedValue{
		"id": StringValue("server-1"),
	})
	identity := validOperationIdentity()
	return ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  &outputs,
		Identity: &identity,
	}
}

func TestResourceObservationValidation(t *testing.T) {
	present := presentOperationObservation(t)
	absent := ResourceObservation{Status: ObservationAbsent}
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	tests := []struct {
		name        string
		observation ResourceObservation
		message     string
	}{
		{name: "present", observation: present},
		{name: "absent", observation: absent},
		{
			name:        "unknown status",
			observation: ResourceObservation{Status: "other"},
			message:     "unknown observation status",
		},
		{
			name:        "present without outputs",
			observation: ResourceObservation{Status: ObservationPresent, Identity: present.Identity},
			message:     "present observation requires outputs",
		},
		{
			name:        "present without identity",
			observation: ResourceObservation{Status: ObservationPresent, Outputs: present.Outputs},
			message:     "present observation requires identity",
		},
		{
			name: "present with pending outputs",
			observation: ResourceObservation{
				Status:   ObservationPresent,
				Outputs:  &pending,
				Identity: present.Identity,
			},
			message: "observation outputs must be concrete",
		},
		{
			name: "absent with fields",
			observation: ResourceObservation{
				Status:   ObservationAbsent,
				Outputs:  present.Outputs,
				Identity: present.Identity,
			},
			message: "absent observation forbids outputs and identity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.observation.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestPlannedResourceTargetValidation(t *testing.T) {
	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	tests := []struct {
		name    string
		change  func(*PlannedResourceTarget)
		message string
	}{
		{name: "concrete"},
		{
			name: "pending input and configuration",
			change: func(target *PlannedResourceTarget) {
				target.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
				target.Configuration = pendingOperationConfiguration()
			},
		},
		{
			name:    "binding",
			change:  func(target *PlannedResourceTarget) { target.Binding.Export = "" },
			message: "binding: export is required",
		},
		{
			name:    "input root",
			change:  func(target *PlannedResourceTarget) { target.Inputs = StringValue("bad") },
			message: "inputs must be an object",
		},
		{
			name: "configuration library",
			change: func(target *PlannedResourceTarget) {
				target.Binding.LibraryPath = "example.com/other"
			},
			message: "configuration library path does not match binding",
		},
		{
			name: "configuration",
			change: func(target *PlannedResourceTarget) {
				target.Configuration = PlannedConfiguration{Kind: "other"}
			},
			message: "configuration: unknown planned configuration kind",
		},
		{
			name: "unsorted sensitive paths",
			change: func(target *PlannedResourceTarget) {
				target.SensitiveInputPaths = []string{"/size", "/name"}
			},
			message: "sensitive input paths must be unique and sorted",
		},
		{
			name: "invalid sensitive path",
			change: func(target *PlannedResourceTarget) {
				target.SensitiveOutputPaths = []string{"id"}
			},
			message: "sensitive output path \"id\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := validPlannedResourceTarget(t)
			if tt.change != nil {
				tt.change(&target)
			}
			err := target.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func validResourcePlanOperation(t *testing.T, decision Decision) ResourcePlanOperation {
	t.Helper()
	desired := validPlannedResourceTarget(t)
	prior := validOperationResourceTarget(t)
	present := presentOperationObservation(t)
	switch decision {
	case DecisionCreate:
		return ResourcePlanOperation{
			Decision: DecisionCreate,
			Desired:  &desired,
			Reasons:  []string{},
		}
	case DecisionUpdate:
		return ResourcePlanOperation{
			Decision:    DecisionUpdate,
			Desired:     &desired,
			Prior:       &prior,
			Observation: &present,
			Reasons:     []string{},
		}
	case DecisionReplace:
		return ResourcePlanOperation{
			Decision:    DecisionReplace,
			Desired:     &desired,
			Prior:       &prior,
			Observation: &present,
			Reasons:     []string{"input:size"},
		}
	case DecisionDestroy:
		absent := ResourceObservation{Status: ObservationAbsent}
		return ResourcePlanOperation{
			Decision:    DecisionDestroy,
			Prior:       &prior,
			Observation: &absent,
			Reasons:     []string{},
		}
	case DecisionNoOp:
		return ResourcePlanOperation{
			Decision:    DecisionNoOp,
			Desired:     &desired,
			Prior:       &prior,
			Observation: &present,
			Reasons:     []string{},
		}
	default:
		return ResourcePlanOperation{Decision: decision}
	}
}

func remoteMissingResourceOperation(t *testing.T) ResourcePlanOperation {
	t.Helper()
	operation := validResourcePlanOperation(t, DecisionCreate)
	prior := validOperationResourceTarget(t)
	absent := ResourceObservation{Status: ObservationAbsent}
	operation.Prior = &prior
	operation.Observation = &absent
	operation.Reasons = []string{"remote-missing"}
	return operation
}

func TestResourcePlanOperationAcceptsDecisionMatrix(t *testing.T) {
	for _, decision := range []Decision{
		DecisionCreate,
		DecisionUpdate,
		DecisionReplace,
		DecisionDestroy,
		DecisionNoOp,
	} {
		t.Run(string(decision), func(t *testing.T) {
			require.NoError(t, validResourcePlanOperation(t, decision).Validate())
		})
	}
	require.NoError(t, remoteMissingResourceOperation(t).Validate())
}

func TestResourcePlanOperationRejectsInvalidDecisionFields(t *testing.T) {
	tests := []struct {
		name    string
		build   func() ResourcePlanOperation
		message string
	}{
		{
			name:    "unknown decision",
			build:   func() ResourcePlanOperation { return ResourcePlanOperation{Decision: "other"} },
			message: "resource decision is invalid",
		},
		{
			name: "initial create with prior",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionCreate)
				prior := validOperationResourceTarget(t)
				operation.Prior = &prior
				return operation
			},
			message: "initial create forbids prior target and observation",
		},
		{
			name: "remote missing without prior",
			build: func() ResourcePlanOperation {
				operation := remoteMissingResourceOperation(t)
				operation.Prior = nil
				return operation
			},
			message: "remote-missing create requires prior target",
		},
		{
			name: "update with absent observation",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionUpdate)
				operation.Observation = &ResourceObservation{Status: ObservationAbsent}
				return operation
			},
			message: "update requires a present observation",
		},
		{
			name: "replace without reasons",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionReplace)
				operation.Reasons = []string{}
				return operation
			},
			message: "replace requires at least one reason",
		},
		{
			name: "replace with unsorted reasons",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionReplace)
				operation.Reasons = []string{"input:size", "binding"}
				return operation
			},
			message: "reasons must be unique and sorted",
		},
		{
			name: "replace with invalid reason",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionReplace)
				operation.Reasons = []string{"changed"}
				return operation
			},
			message: "replacement reason is invalid",
		},
		{
			name: "pending replacement without reason",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionReplace)
				operation.Desired.Configuration = pendingOperationConfiguration()
				return operation
			},
			message: "pending replacement configuration requires configuration-pending reason",
		},
		{
			name: "pending reason with concrete configuration",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionReplace)
				operation.Reasons = []string{"configuration-pending"}
				return operation
			},
			message: "configuration-pending reason requires pending configuration",
		},
		{
			name: "update with pending configuration",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionUpdate)
				operation.Desired.Configuration = pendingOperationConfiguration()
				return operation
			},
			message: "update requires concrete desired configuration",
		},
		{
			name: "destroy with desired target",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionDestroy)
				desired := validPlannedResourceTarget(t)
				operation.Desired = &desired
				return operation
			},
			message: "destroy forbids desired target",
		},
		{
			name: "no-op with reason",
			build: func() ResourcePlanOperation {
				operation := validResourcePlanOperation(t, DecisionNoOp)
				operation.Reasons = []string{"binding"}
				return operation
			},
			message: "no-op forbids reasons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.build().Validate(), tt.message)
		})
	}
}

func validPlannedActionTarget(t *testing.T) PlannedActionTarget {
	t.Helper()
	return PlannedActionTarget{
		Binding:              validOperationBinding("notify"),
		Inputs:               operationObject(t, map[string]EncodedValue{"message": StringValue("ok")}),
		Configuration:        concreteOperationConfiguration(t),
		TriggerHash:          "trigger-1",
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func operationActionState(t *testing.T) ActionStatePayload {
	t.Helper()
	return ActionStatePayload{
		Binding:              validOperationBinding("notify"),
		Inputs:               operationObject(t, map[string]EncodedValue{"message": StringValue("ok")}),
		Outputs:              operationObject(t, map[string]EncodedValue{"sent": BooleanValue(true)}),
		Configuration:        validConfigurationRecord(t),
		TriggerHash:          "trigger-1",
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func TestActionPlanOperationValidation(t *testing.T) {
	desired := validPlannedActionTarget(t)
	prior := operationActionState(t)
	tests := []struct {
		name      string
		operation ActionPlanOperation
		message   string
	}{
		{
			name: "rerun",
			operation: ActionPlanOperation{
				Decision: DecisionRerun,
				Desired:  &desired,
			},
		},
		{
			name: "skip",
			operation: ActionPlanOperation{
				Decision: DecisionSkip,
				Desired:  &desired,
				Prior:    &prior,
			},
		},
		{
			name: "destroy",
			operation: ActionPlanOperation{
				Decision: DecisionDestroy,
				Prior:    &prior,
			},
		},
		{
			name:      "unknown decision",
			operation: ActionPlanOperation{Decision: DecisionCreate},
			message:   "action decision is invalid",
		},
		{
			name:      "rerun without desired",
			operation: ActionPlanOperation{Decision: DecisionRerun},
			message:   "rerun requires desired target",
		},
		{
			name: "skip without prior",
			operation: ActionPlanOperation{
				Decision: DecisionSkip,
				Desired:  &desired,
			},
			message: "skip requires prior state",
		},
		{
			name: "skip with changed trigger",
			operation: func() ActionPlanOperation {
				changed := desired
				changed.TriggerHash = "trigger-2"
				return ActionPlanOperation{
					Decision: DecisionSkip,
					Desired:  &changed,
					Prior:    &prior,
				}
			}(),
			message: "skip requires equal non-empty trigger hashes",
		},
		{
			name: "destroy with desired",
			operation: ActionPlanOperation{
				Decision: DecisionDestroy,
				Desired:  &desired,
				Prior:    &prior,
			},
			message: "destroy forbids desired target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func validPlannedDataSourceTarget(t *testing.T) PlannedDataSourceTarget {
	t.Helper()
	return PlannedDataSourceTarget{
		Binding:              validOperationBinding("image"),
		Inputs:               operationObject(t, map[string]EncodedValue{"name": StringValue("base")}),
		Configuration:        concreteOperationConfiguration(t),
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func operationDataSourceState(t *testing.T) DataSourceStatePayload {
	t.Helper()
	return DataSourceStatePayload{
		Binding:              validOperationBinding("image"),
		Inputs:               operationObject(t, map[string]EncodedValue{"name": StringValue("base")}),
		Outputs:              operationObject(t, map[string]EncodedValue{"id": StringValue("ami-1")}),
		Configuration:        validConfigurationRecord(t),
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func TestDataSourcePlanOperationValidation(t *testing.T) {
	desired := validPlannedDataSourceTarget(t)
	prior := operationDataSourceState(t)
	outputs := operationObject(t, map[string]EncodedValue{"id": StringValue("ami-1")})
	concrete := DataSourcePlanOperation{
		Decision:        DecisionRead,
		Desired:         &desired,
		ObservedOutputs: &outputs,
	}
	require.NoError(t, concrete.Validate())

	missingOutputs := concrete
	missingOutputs.ObservedOutputs = nil
	require.ErrorContains(t, missingOutputs.Validate(), "concrete read requires observed outputs")

	pending, err := PendingEncodedValue([]string{"resource.network.id"})
	require.NoError(t, err)
	pendingDesired := desired
	pendingDesired.Inputs = operationObject(t, map[string]EncodedValue{"name": pending})
	pendingRead := DataSourcePlanOperation{Decision: DecisionRead, Desired: &pendingDesired}
	require.NoError(t, pendingRead.Validate())
	pendingRead.ObservedOutputs = &outputs
	require.ErrorContains(t, pendingRead.Validate(), "pending read forbids observed outputs")

	destroy := DataSourcePlanOperation{Decision: DecisionDestroy, Prior: &prior}
	require.NoError(t, destroy.Validate())
	destroy.Desired = &desired
	require.ErrorContains(t, destroy.Validate(), "destroy forbids desired target")
}

func TestLibraryConfigurationPlanOperationValidation(t *testing.T) {
	concreteInputs := operationObject(t, map[string]EncodedValue{"region": StringValue("east")})
	concrete := LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   concreteInputs,
		Result:   concreteOperationConfiguration(t),
	}
	require.NoError(t, concrete.Validate())

	pending, err := PendingEncodedValue([]string{"resource.a.id", "resource.b.id"})
	require.NoError(t, err)
	pendingInputs := operationObject(t, map[string]EncodedValue{"value": pending})
	pendingOperation := LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   pendingInputs,
		Result: PlannedConfiguration{
			Kind:        PlannedConfigurationPending,
			PendingRefs: []string{"resource.a.id", "resource.b.id"},
		},
	}
	require.NoError(t, pendingOperation.Validate())

	pendingOperation.Result.PendingRefs = []string{"resource.a.id"}
	require.ErrorContains(
		t,
		pendingOperation.Validate(),
		"pending result references do not match inputs",
	)

	concrete.Result = pendingOperation.Result
	require.ErrorContains(t, concrete.Validate(), "concrete inputs require concrete result")
}

func operationCompositeState(t *testing.T, category NodeKind) CompositeStatePayload {
	t.Helper()
	return CompositeStatePayload{
		Category: string(category),
		Binding: Binding{
			LibraryPath: "example.com/composites",
			Export:      "application",
		},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("api"),
		}),
		Outputs: operationObject(t, map[string]EncodedValue{
			"url": StringValue("https://example.com"),
		}),
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func TestCompositePlanOperationValidation(t *testing.T) {
	desired := PlannedCompositeTarget{
		Category: NodeResource,
		Binding: Binding{
			LibraryPath: "example.com/composites",
			Export:      "application",
		},
		Inputs: operationObject(t, map[string]EncodedValue{
			"name": StringValue("api"),
		}),
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
	eval := CompositePlanOperation{Decision: DecisionEval, Desired: &desired}
	require.NoError(t, eval.Validate(NodeResource))
	require.ErrorContains(
		t,
		eval.Validate(NodeAction),
		"desired category resource does not match action",
	)

	prior := operationCompositeState(t, NodeResource)
	destroy := CompositePlanOperation{Decision: DecisionDestroy, Prior: &prior}
	require.NoError(t, destroy.Validate(NodeResource))
	require.ErrorContains(
		t,
		destroy.Validate(NodeAction),
		"prior category resource does not match action",
	)
}

func TestOutputPlanOperationValidation(t *testing.T) {
	operation := OutputPlanOperation{
		Decision:  DecisionEval,
		Value:     StringValue("value"),
		Sensitive: true,
	}
	require.NoError(t, operation.Validate())
	operation.Decision = DecisionRead
	require.ErrorContains(t, operation.Validate(), "output decision must be eval")
}

func validStepOperations(t *testing.T) map[NodeKind]StepOperation {
	t.Helper()
	resource := validResourcePlanOperation(t, DecisionCreate)
	actionDesired := validPlannedActionTarget(t)
	action := ActionPlanOperation{Decision: DecisionRerun, Desired: &actionDesired}
	dataDesired := validPlannedDataSourceTarget(t)
	dataOutputs := operationObject(t, map[string]EncodedValue{"id": StringValue("ami-1")})
	dataSource := DataSourcePlanOperation{
		Decision:        DecisionRead,
		Desired:         &dataDesired,
		ObservedOutputs: &dataOutputs,
	}
	configuration := LibraryConfigurationPlanOperation{
		Decision: DecisionEval,
		Inputs:   operationObject(t, map[string]EncodedValue{"region": StringValue("east")}),
		Result:   concreteOperationConfiguration(t),
	}
	output := OutputPlanOperation{Decision: DecisionEval, Value: StringValue("ok")}
	return map[NodeKind]StepOperation{
		NodeResource: {
			Kind:     StepResource,
			Resource: &resource,
		},
		NodeAction: {
			Kind:   StepAction,
			Action: &action,
		},
		NodeDataSource: {
			Kind:       StepDataSource,
			DataSource: &dataSource,
		},
		NodeLibraryConfiguration: {
			Kind:                 StepLibraryConfiguration,
			LibraryConfiguration: &configuration,
		},
		NodeOutput: {
			Kind:   StepOutput,
			Output: &output,
		},
	}
}

func TestStepOperationValidation(t *testing.T) {
	for kind, operation := range validStepOperations(t) {
		t.Run(string(kind), func(t *testing.T) {
			require.NoError(t, operation.Validate(kind))
		})
	}

	resource := validResourcePlanOperation(t, DecisionCreate)
	actionDesired := validPlannedActionTarget(t)
	action := ActionPlanOperation{Decision: DecisionRerun, Desired: &actionDesired}
	extra := StepOperation{
		Kind:     StepResource,
		Resource: &resource,
		Action:   &action,
	}
	require.ErrorContains(t, extra.Validate(NodeResource), "resource operation forbids other payloads")

	mismatch := StepOperation{Kind: StepResource, Resource: &resource}
	require.ErrorContains(
		t,
		mismatch.Validate(NodeAction),
		"resource operation does not match action step",
	)
}

func TestPlanStepV2Validation(t *testing.T) {
	operation := validStepOperations(t)[NodeResource]
	step := PlanStepV2{
		Address:   "resource.api",
		Kind:      NodeResource,
		DependsOn: []string{"resource.network"},
		Operation: operation,
	}
	require.NoError(t, step.Validate())

	step.DependsOn = []string{"resource.z", "resource.a"}
	require.ErrorContains(t, step.Validate(), "dependencies must be unique and sorted")

	step.DependsOn = []string{}
	step.Address = "action.api"
	require.ErrorContains(t, step.Validate(), "address category action does not match resource")
}

func TestPlanStepV2AcceptsEveryNodeAddress(t *testing.T) {
	addresses := map[NodeKind]string{
		NodeResource:             "resource.api",
		NodeAction:               "action.notify",
		NodeDataSource:           "data-source.image",
		NodeLibraryConfiguration: "library-config.cloud",
		NodeOutput:               "output.url",
	}
	for kind, operation := range validStepOperations(t) {
		t.Run(string(kind), func(t *testing.T) {
			step := PlanStepV2{
				Address:   addresses[kind],
				Kind:      kind,
				DependsOn: []string{},
				Operation: operation,
			}
			require.NoError(t, step.Validate())
		})
	}
}

func TestStateRefV2Validation(t *testing.T) {
	ref := StateRefV2{
		Name: "local",
		Body: operationObject(t, map[string]EncodedValue{}),
	}
	require.NoError(t, ref.Validate())

	ref.Name = ""
	require.ErrorContains(t, ref.Validate(), "state backend name is required")
}

func validPlanFileV2(t *testing.T) PlanFileV2 {
	t.Helper()
	plan := PlanFileV2{
		FormatVersion: PlanFormatVersion,
		Factory: FactoryRef{
			Name:            "deploy",
			Version:         "v1.0.0",
			ContentRevision: "revision-1",
		},
		Stack:         "production",
		StateRevision: "state-1",
		GeneratedAt:   time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		Inputs:        operationObject(t, map[string]EncodedValue{"region": StringValue("east")}),
		Parallelism:   4,
		Mode:          PlanApply,
		StateMoves:    []PlannedEntryMove{},
		Steps: []PlanStepV2{
			{
				Address:   "resource.api",
				Kind:      NodeResource,
				DependsOn: []string{},
				Operation: validStepOperations(t)[NodeResource],
			},
		},
	}
	digest, err := planFileV2Digest(plan)
	require.NoError(t, err)
	plan.Digest = digest
	return plan
}

func TestPlanFileV2Validation(t *testing.T) {
	plan := validPlanFileV2(t)
	require.NoError(t, plan.Validate())

	plan.Digest = strings.Repeat("f", 64)
	require.ErrorContains(t, plan.Validate(), "digest does not match plan contents")
}

func TestPlanFileV2RejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*PlanFileV2)
		message string
	}{
		{
			name:    "format version",
			change:  func(plan *PlanFileV2) { plan.FormatVersion = 1 },
			message: "format version must be 2",
		},
		{
			name:    "factory",
			change:  func(plan *PlanFileV2) { plan.Factory.ContentRevision = "" },
			message: "factory content revision is required",
		},
		{
			name:    "stack",
			change:  func(plan *PlanFileV2) { plan.Stack = "" },
			message: "stack is required",
		},
		{
			name:    "generated time",
			change:  func(plan *PlanFileV2) { plan.GeneratedAt = time.Time{} },
			message: "generated time is required",
		},
		{
			name:    "input root",
			change:  func(plan *PlanFileV2) { plan.Inputs = StringValue("bad") },
			message: "inputs must be an object",
		},
		{
			name: "backend",
			change: func(plan *PlanFileV2) {
				plan.Backend = &StateRefV2{Body: operationObject(t, map[string]EncodedValue{})}
			},
			message: "state backend name is required",
		},
		{
			name:    "mode",
			change:  func(plan *PlanFileV2) { plan.Mode = "other" },
			message: "plan mode is invalid",
		},
		{
			name:    "missing moves",
			change:  func(plan *PlanFileV2) { plan.StateMoves = nil },
			message: "state moves are required",
		},
		{
			name: "equal move endpoints",
			change: func(plan *PlanFileV2) {
				plan.StateMoves = []PlannedEntryMove{{
					From: "resource.api",
					To:   "resource.api",
				}}
			},
			message: "endpoints must differ",
		},
		{
			name:    "missing steps",
			change:  func(plan *PlanFileV2) { plan.Steps = nil },
			message: "steps are required",
		},
		{
			name: "duplicate steps",
			change: func(plan *PlanFileV2) {
				plan.Steps = append(plan.Steps, plan.Steps[0])
			},
			message: "steps contain duplicate address",
		},
		{
			name:    "digest syntax",
			change:  func(plan *PlanFileV2) { plan.Digest = "bad" },
			message: "digest must be a lowercase SHA-256 digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validPlanFileV2(t)
			tt.change(&plan)
			require.ErrorContains(t, plan.Validate(), tt.message)
		})
	}
}
