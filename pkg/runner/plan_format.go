package runner

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type planDecisionSummary struct {
	Create  int `json:"create"  ub:"create"`
	Read    int `json:"read"    ub:"read"`
	Update  int `json:"update"  ub:"update"`
	Replace int `json:"replace" ub:"replace"`
	Destroy int `json:"destroy" ub:"destroy"`
	Rerun   int `json:"rerun"   ub:"rerun"`
	Skip    int `json:"skip"    ub:"skip"`
	NoOp    int `json:"no-op"   ub:"no-op"`
	Eval    int `json:"eval"    ub:"eval"`
}

type planStateMove struct {
	From string `json:"from" ub:"from"`
	To   string `json:"to"   ub:"to"`
}

type planSummaryStep struct {
	Address         string   `json:"address"          ub:"address"`
	Category        string   `json:"category"         ub:"category"`
	Decision        string   `json:"decision"         ub:"decision"`
	Composite       bool     `json:"composite"        ub:"composite"`
	Drift           bool     `json:"drift"            ub:"drift"`
	Gone            bool     `json:"gone"             ub:"gone"`
	ReplaceTriggers []string `json:"replace-triggers" ub:"replace-triggers"`
	DeferredConfig  *string  `json:"deferred-config"  ub:"deferred-config"`
}

type planSummaryResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	PlanDigest    *string                 `json:"plan-digest"    ub:"plan-digest"`
	File          *filechange.Change      `json:"file"           ub:"file"`
	StateRev      *string                 `json:"state-rev"      ub:"state-rev"`
	Parallelism   int                     `json:"parallelism"    ub:"parallelism"`
	Destroy       bool                    `json:"destroy"        ub:"destroy"`
	Summary       planDecisionSummary     `json:"summary"        ub:"summary"`
	StateMoves    []planStateMove         `json:"state-moves"    ub:"state-moves"`
	Steps         []planSummaryStep       `json:"steps"          ub:"steps"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func buildPlanSummary(
	info Info,
	plan *runtime.Plan,
	planDigest *string,
	file *filechange.Change,
	diagnostics []diagnostic.Diagnostic,
) (planSummaryResult, error) {
	if plan == nil {
		return planSummaryResult{}, fmt.Errorf("plan summary: plan is required")
	}
	if (planDigest == nil) != (file == nil) {
		return planSummaryResult{}, fmt.Errorf(
			"plan summary: plan digest and artifact file must both be present",
		)
	}
	result := planSummaryResult{
		Kind:          "plan-summary",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         plan.Stack,
		Parallelism:   plan.Parallelism,
		Destroy:       plan.Destroy,
		StateMoves:    make([]planStateMove, 0, len(plan.StateMoves)),
		Steps:         make([]planSummaryStep, 0, len(plan.Steps)),
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
	if result.Parallelism <= 0 {
		result.Parallelism = runtime.DefaultParallelism
	}
	if planDigest != nil {
		value := *planDigest
		result.PlanDigest = &value
	}
	if file != nil {
		changes, err := filechange.Compose([]filechange.Change{*file})
		if err != nil {
			return planSummaryResult{}, err
		}
		if len(changes) != 1 {
			return planSummaryResult{}, fmt.Errorf("plan summary: artifact change is empty")
		}
		result.File = &changes[0]
	}
	if plan.StateRev != "" {
		value := plan.StateRev
		result.StateRev = &value
	}
	for _, move := range plan.StateMoves {
		result.StateMoves = append(result.StateMoves, planStateMove{
			From: move.From,
			To:   move.To,
		})
	}
	for _, step := range plan.Steps {
		if step == nil {
			return planSummaryResult{}, fmt.Errorf("plan summary: nil step")
		}
		if err := incrementPlanDecision(&result.Summary, step.Decision); err != nil {
			return planSummaryResult{}, err
		}
		triggers := slices.Clone(step.ReplaceTriggers)
		if triggers == nil {
			triggers = []string{}
		}
		slices.Sort(triggers)
		var deferred *string
		if step.DeferredConfig != "" {
			value := step.DeferredConfig
			deferred = &value
		}
		result.Steps = append(result.Steps, planSummaryStep{
			Address:         step.Address,
			Category:        string(step.Kind),
			Decision:        string(step.Decision),
			Composite:       step.Composite,
			Drift:           step.Drift(),
			Gone:            step.Gone() || step.AlreadyGone,
			ReplaceTriggers: triggers,
			DeferredConfig:  deferred,
		})
	}
	slices.SortFunc(result.Steps, func(a, b planSummaryStep) int {
		return cmp.Compare(a.Address, b.Address)
	})
	return result, nil
}

func incrementPlanDecision(summary *planDecisionSummary, decision runtime.Decision) error {
	switch decision {
	case runtime.DecisionCreate:
		summary.Create++
	case runtime.DecisionRead:
		summary.Read++
	case runtime.DecisionUpdate:
		summary.Update++
	case runtime.DecisionReplace:
		summary.Replace++
	case runtime.DecisionDestroy:
		summary.Destroy++
	case runtime.DecisionRerun:
		summary.Rerun++
	case runtime.DecisionSkip:
		summary.Skip++
	case runtime.DecisionNoOp:
		summary.NoOp++
	case runtime.DecisionEval:
		summary.Eval++
	default:
		return fmt.Errorf("plan summary: unsupported decision %q", decision)
	}
	return nil
}
