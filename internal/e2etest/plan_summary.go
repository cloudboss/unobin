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
	Address  string `json:"address"`
	Kind     string `json:"kind"`
	Decision string `json:"decision"`
}

func comparePlanSummaries(
	caseDir string,
	workspace string,
	checks []PlanSummaryCheck,
	doUpdate bool,
) error {
	for _, check := range checks {
		body, err := planSummaryJSON(workspace, check.Path)
		if err != nil {
			return err
		}
		if err := compareOptionalGolden(caseDir, check.Path, check.Want, body, doUpdate); err != nil {
			return err
		}
	}
	return nil
}

func planSummaryJSON(workspace string, relPath string) (string, error) {
	path := filepath.Join(workspace, filepath.FromSlash(relPath))
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read plan %s: %w", relPath, err)
	}
	pf, err := runtime.OpenPlan(body, planSummaryEncrypter)
	if err != nil {
		return "", fmt.Errorf("open plan %s: %w", relPath, err)
	}
	summary := summarizePlan(pf)
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

func summarizePlan(pf *runtime.PlanFile) planSummary {
	factory := pf.Factory
	factory.ContentRevision = "<revision>"
	steps := make([]planStepSummary, 0, len(pf.Steps))
	for _, step := range pf.Steps {
		steps = append(steps, planStepSummary{
			Address:  step.Address,
			Kind:     string(step.Kind),
			Decision: string(step.Decision),
		})
	}
	return planSummary{
		FormatVersion: pf.FormatVersion,
		Factory:       factory,
		Stack:         pf.Stack,
		Parallelism:   pf.Parallelism,
		Destroy:       pf.Destroy,
		Steps:         steps,
	}
}
