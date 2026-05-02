package runtime

import (
	"encoding/json"
	"fmt"
	"time"
)

// PlanFormatVersion is the schema version this package reads and writes
// for plan files.
const PlanFormatVersion = 1

// PlanFile is the on-disk shape of a plan. Steps in the file mirror
// the in-memory `Plan.Steps` but use `json` tags for stable
// serialization.
type PlanFile struct {
	FormatVersion int        `json:"format-version"`
	Stack         StackRef   `json:"stack"`
	DeploymentID  string     `json:"deployment-id"`
	StateRev      string     `json:"state-rev"`
	GeneratedAt   time.Time  `json:"generated-at"`
	Steps         []PlanStep `json:"steps"`
}

// StackRef identifies the stack a plan was computed against.
type StackRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// EncodePlan renders a plan as JSON bytes for on-disk storage.
func EncodePlan(p *Plan) ([]byte, error) {
	steps := make([]PlanStep, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = *s
	}
	pf := PlanFile{
		FormatVersion: PlanFormatVersion,
		Stack: StackRef{
			Name:    p.Stack.Name,
			Version: p.Stack.Version,
			Commit:  p.Stack.Commit,
		},
		DeploymentID: p.DeploymentID,
		StateRev:     p.StateRev,
		GeneratedAt:  time.Now().UTC(),
		Steps:        steps,
	}
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return append(b, '\n'), nil
}

// DecodePlan parses a plan file from JSON bytes.
func DecodePlan(b []byte) (*PlanFile, error) {
	var pf PlanFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if pf.FormatVersion != PlanFormatVersion {
		return nil, fmt.Errorf("plan: unsupported format-version %d (this build expects %d)",
			pf.FormatVersion, PlanFormatVersion)
	}
	return &pf, nil
}
