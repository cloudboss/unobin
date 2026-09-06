package e2etest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

type planSummary struct {
	FormatVersion int                `json:"format-version"`
	Factory       runtime.FactoryRef `json:"factory"`
	Stack         string             `json:"stack"`
	Parallelism   int                `json:"parallelism,omitempty"`
	Destroy       bool               `json:"destroy,omitempty"`
	Steps         []planStepSummary  `json:"steps"`
}

type planStepSummary struct {
	Address  string         `json:"address"`
	Kind     string         `json:"kind"`
	Decision string         `json:"decision"`
	Inputs   map[string]any `json:"inputs,omitempty"`
}

func comparePlanSummaries(
	caseDir string,
	workspace string,
	checks []PlanSummaryCheck,
	doUpdate bool,
) error {
	for _, check := range checks {
		body, err := planSummaryJSON(workspace, check.Path, check.IncludeInputs)
		if err != nil {
			return err
		}
		if err := compareOptionalGolden(caseDir, check.Path, check.Want, body, doUpdate); err != nil {
			return err
		}
	}
	return nil
}

func planSummaryJSON(
	workspace string,
	relPath string,
	includeInputs bool,
) (string, error) {
	path := filepath.Join(workspace, filepath.FromSlash(relPath))
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read plan %s: %w", relPath, err)
	}
	pf, err := runtime.OpenPlanV2(body, planSummaryEncrypter)
	if err != nil {
		return "", fmt.Errorf("open plan %s: %w", relPath, err)
	}
	summary := summarizePlan(&pf, includeInputs)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(summary); err != nil {
		return "", err
	}
	return out.String(), nil
}

func planSummaryEncrypter(ref *runtime.StateRef) (encrypt.Encrypter, error) {
	if ref != nil && ref.Name != "noop" {
		return nil, fmt.Errorf("unsupported plan encrypter %s", ref.Name)
	}
	return encrypters.Noop{}, nil
}

func summarizePlan(pf *runtime.PlanFileV2, includeInputs bool) planSummary {
	factory := pf.Factory
	factory.ContentRevision = "<revision>"
	steps := make([]planStepSummary, 0, len(pf.Steps))
	for _, step := range pf.Steps {
		summary := planStepSummary{
			Address: step.Address,
			Kind:    string(step.Kind),
		}
		var decision runtime.Decision
		var inputs runtime.EncodedValue
		switch op := step.Operation; op.Kind {
		case runtime.StepResource:
			decision = op.Resource.Decision
			if op.Resource.Desired != nil {
				inputs = op.Resource.Desired.Inputs
			}
		case runtime.StepAction:
			decision = op.Action.Decision
			if op.Action.Desired != nil {
				inputs = op.Action.Desired.Inputs
			}
		case runtime.StepDataSource:
			decision = op.DataSource.Decision
			if op.DataSource.Desired != nil {
				inputs = op.DataSource.Desired.Inputs
			}
		case runtime.StepComposite:
			decision = op.Composite.Decision
			if op.Composite.Desired != nil {
				inputs = op.Composite.Desired.Inputs
			}
		case runtime.StepLibraryConfiguration:
			decision, inputs = op.LibraryConfiguration.Decision, op.LibraryConfiguration.Inputs
		case runtime.StepOutput:
			decision = op.Output.Decision
		}
		summary.Decision = string(decision)
		if includeInputs {
			summary.Inputs = summaryObject(inputs)
		}
		steps = append(steps, summary)
	}
	return planSummary{
		FormatVersion: pf.FormatVersion,
		Factory:       factory,
		Stack:         pf.Stack,
		Parallelism:   pf.Parallelism,
		Destroy:       pf.Mode == runtime.PlanDestroy,
		Steps:         steps,
	}
}
