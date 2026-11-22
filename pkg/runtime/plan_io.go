package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudboss/unobin/internal/jsonvalue"
	"github.com/cloudboss/unobin/pkg/diagnostic"
)

// PlanFormatVersion is the schema version this package reads and writes
// for plan files.
const PlanFormatVersion = 2

// PlanFile is the on-disk form of a plan. Steps in the file mirror the
// in-memory Plan.Steps. Inputs holds the validated root inputs the plan was
// computed against, so apply can seed them into its eval scope without reading
// the stack file again.
type PlanFile struct {
	FormatVersion int            `json:"format-version"`
	Factory       FactoryRef     `json:"factory"`
	Stack         string         `json:"stack"`
	StateRev      string         `json:"state-rev"`
	GeneratedAt   time.Time      `json:"generated-at"`
	Inputs        map[string]any `json:"inputs,omitempty"`

	Backend     *StateRef          `json:"backend,omitempty"`
	Parallelism int                `json:"parallelism,omitempty"`
	Destroy     bool               `json:"destroy,omitempty"`
	StateMoves  []PlannedEntryMove `json:"state-moves,omitempty"`
	Steps       []PlanStep         `json:"steps"`
}

// FactoryRef identifies the stack a plan was computed against.
type FactoryRef struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}

// EncodePlan renders a plan as JSON bytes for on-disk storage.
func EncodePlan(p *Plan) ([]byte, error) {
	steps := make([]PlanStep, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = *s
	}
	pf := PlanFile{
		FormatVersion: PlanFormatVersion,
		Factory: FactoryRef{
			Name:            p.Factory.Name,
			Version:         p.Factory.Version,
			ContentRevision: p.Factory.ContentRevision,
		},
		Stack:       p.Stack,
		StateRev:    p.StateRev,
		GeneratedAt: time.Now().UTC(),
		Inputs:      p.Inputs,
		Backend:     p.Backend,
		Parallelism: p.Parallelism,
		Destroy:     p.Destroy,
		StateMoves:  p.StateMoves,
		Steps:       steps,
	}
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return nil, diagnostic.Context("plan", err)
	}
	return append(b, '\n'), nil
}

// DecodePlan parses a plan file from JSON bytes. JSON has no native
// distinction between int and float. DecodePlan restores integer tokens
// as int64 when they fit and decodes decimal and exponent tokens as float64.
func DecodePlan(b []byte) (*PlanFile, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var pf PlanFile
	if err := dec.Decode(&pf); err != nil {
		return nil, diagnostic.Context("plan", err)
	}
	if pf.FormatVersion != PlanFormatVersion {
		return nil, fmt.Errorf(
			"plan: unsupported format-version %d (this build expects %d); recreate the plan",
			pf.FormatVersion,
			PlanFormatVersion,
		)
	}
	jsonvalue.CoerceNumbers(pf.Inputs)
	if pf.Backend != nil {
		jsonvalue.CoerceNumbers(pf.Backend.Body)
	}
	for i := range pf.Steps {
		s := &pf.Steps[i]
		jsonvalue.CoerceNumbers(s.Inputs)
		jsonvalue.CoerceNumbers(s.Configuration)
		jsonvalue.CoerceNumbers(s.PriorInputs)
		jsonvalue.CoerceNumbers(s.PriorConfiguration)
		jsonvalue.CoerceNumbers(s.PriorOutputs)
		jsonvalue.CoerceNumbers(s.ObservedOutputs)
	}
	return &pf, nil
}
