package runner

import (
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"
)

type planView struct {
	Stack       string
	StateRev    string
	Parallelism int
	Destroy     bool
	StateMoves  []runtime.PlannedEntryMove
	Steps       []*planStepView
}

type planStepView struct {
	Address          string
	Kind             runtime.NodeKind
	Composite        bool
	Decision         runtime.Decision
	Inputs           map[string]any
	PriorInputs      map[string]any
	PriorOutputs     map[string]any
	ObservedOutputs  map[string]any
	UnresolvedInputs map[string][]string
	DeferredConfig   string
	ReplaceTriggers  []string
	AlreadyGone      bool
	RemoteMissing    bool
	SensitiveInputs  []string
	SensitiveOutputs []string
}

func newPlanView(plan *runtime.PlanFileV2) *planView {
	view := &planView{
		Stack: plan.Stack, StateRev: plan.StateRevision, Parallelism: plan.Parallelism,
		Destroy: plan.Mode == runtime.PlanDestroy, StateMoves: slices.Clone(plan.StateMoves),
		Steps: make([]*planStepView, len(plan.Steps)),
	}
	for i, step := range plan.Steps {
		view.Steps[i] = newPlanStepView(step)
	}
	return view
}

func newPlanStepView(step runtime.PlanStepV2) *planStepView {
	view := &planStepView{Address: step.Address, Kind: step.Kind,
		Composite: step.Operation.Kind == runtime.StepComposite}
	var inputs, priorInputs, priorOutputs, observedOutputs runtime.EncodedValue
	var inputPaths, outputPaths []string
	var configuration runtime.PlannedConfiguration
	switch op := step.Operation; op.Kind {
	case runtime.StepResource:
		resource := op.Resource
		view.Decision = resource.Decision
		view.ReplaceTriggers = slices.Clone(resource.Reasons)
		if desired := resource.Desired; desired != nil {
			inputs, configuration = desired.Inputs, desired.Configuration
			inputPaths = append(inputPaths, desired.SensitiveInputPaths...)
			outputPaths = append(outputPaths, desired.SensitiveOutputPaths...)
		}
		if prior := resource.Prior; prior != nil {
			priorInputs, priorOutputs = prior.Inputs, prior.Outputs
			inputPaths = append(inputPaths, prior.SensitiveInputPaths...)
			outputPaths = append(outputPaths, prior.SensitiveOutputPaths...)
		}
		if observation := resource.Observation; observation != nil {
			if observation.Outputs != nil {
				observedOutputs = *observation.Outputs
			}
			view.RemoteMissing = resource.Decision == runtime.DecisionCreate &&
				observation.Status == runtime.ObservationAbsent
			view.AlreadyGone = resource.Decision == runtime.DecisionDestroy &&
				observation.Status == runtime.ObservationAbsent
		}
	case runtime.StepAction:
		action := op.Action
		view.Decision = action.Decision
		if desired := action.Desired; desired != nil {
			inputs, configuration = desired.Inputs, desired.Configuration
			inputPaths = append(inputPaths, desired.SensitiveInputPaths...)
		}
		if prior := action.Prior; prior != nil {
			priorInputs = prior.Inputs
			inputPaths = append(inputPaths, prior.SensitiveInputPaths...)
		}
	case runtime.StepDataSource:
		data := op.DataSource
		view.Decision = data.Decision
		if desired := data.Desired; desired != nil {
			inputs, configuration = desired.Inputs, desired.Configuration
			inputPaths = append(inputPaths, desired.SensitiveInputPaths...)
		}
		if prior := data.Prior; prior != nil {
			priorInputs = prior.Inputs
			inputPaths = append(inputPaths, prior.SensitiveInputPaths...)
		}
	case runtime.StepComposite:
		composite := op.Composite
		view.Decision = composite.Decision
		if desired := composite.Desired; desired != nil {
			inputs = desired.Inputs
			inputPaths = append(inputPaths, desired.SensitiveInputPaths...)
		}
		if prior := composite.Prior; prior != nil {
			priorInputs = prior.Inputs
			inputPaths = append(inputPaths, prior.SensitiveInputPaths...)
		}
	case runtime.StepLibraryConfiguration:
		view.Kind = runtime.NodeLibraryConfig
		view.Decision = op.LibraryConfiguration.Decision
	case runtime.StepOutput:
		view.Decision = op.Output.Decision
	}
	if configuration.Kind == runtime.PlannedConfigurationPending {
		view.DeferredConfig = strings.Join(configuration.PendingRefs, ", ")
	}
	if inputs.Kind() == "" {
		inputs = priorInputs
	}
	view.Inputs = displayObject(inputs, inputPaths)
	view.PriorInputs = displayObject(priorInputs, inputPaths)
	view.PriorOutputs = displayObject(priorOutputs, outputPaths)
	view.ObservedOutputs = displayObject(observedOutputs, outputPaths)
	return view
}

func (s *planStepView) Drift() bool {
	return s.PriorOutputs != nil && s.ObservedOutputs != nil && len(driftedFields(s)) > 0
}

func (s *planStepView) Gone() bool { return s.RemoteMissing }
