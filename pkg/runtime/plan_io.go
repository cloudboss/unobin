package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PlanFormatVersion is the schema version this package reads and writes
// for plan files.
const PlanFormatVersion = 1

// PlanFile is the on-disk shape of a plan. Steps in the file mirror
// the in-memory `Plan.Steps` but use `json` tags for stable
// serialization. Inputs carries the validated root inputs the plan was
// computed against, so apply can seed them into its eval scope without
// re-reading config.ub.
type PlanFile struct {
	FormatVersion     int                       `json:"format-version"`
	Factory           FactoryRef                `json:"factory"`
	Stack             string                    `json:"stack"`
	StateRev          string                    `json:"state-rev"`
	GeneratedAt       time.Time                 `json:"generated-at"`
	Inputs            map[string]any            `json:"inputs,omitempty"`
	RawConfigurations map[string]map[string]any `json:"configurations,omitempty"`
	Backend           *StateRef                 `json:"backend,omitempty"`
	Parallelism       int                       `json:"parallelism,omitempty"`
	Destroy           bool                      `json:"destroy,omitempty"`
	Steps             []PlanStep                `json:"steps"`
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
		Stack:             p.Stack,
		StateRev:          p.StateRev,
		GeneratedAt:       time.Now().UTC(),
		Inputs:            p.Inputs,
		RawConfigurations: p.RawConfigurations,
		Backend:           p.Backend,
		Parallelism:       p.Parallelism,
		Destroy:           p.Destroy,
		Steps:             steps,
	}
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return append(b, '\n'), nil
}

// DecodePlan parses a plan file from JSON bytes. JSON has no native
// distinction between int and float, so the decoder reads numbers as
// json.Number and coerceNumbers walks the result, restoring int64 for
// values whose text has no decimal point or exponent and float64 for
// everything else. This preserves the typing that Plan emitted: a
// re-evaluation at apply time gets int64 back where the schema said
// integer.
func DecodePlan(b []byte) (*PlanFile, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var pf PlanFile
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if pf.FormatVersion != PlanFormatVersion {
		return nil, fmt.Errorf("plan: unsupported format-version %d (this build expects %d)",
			pf.FormatVersion, PlanFormatVersion)
	}
	pf.Inputs = coerceMap(pf.Inputs)
	for k, m := range pf.RawConfigurations {
		pf.RawConfigurations[k] = coerceMap(m)
	}
	if pf.Backend != nil {
		pf.Backend.Body = coerceMap(pf.Backend.Body)
	}
	for i := range pf.Steps {
		s := &pf.Steps[i]
		s.Inputs = coerceMap(s.Inputs)
		s.PriorOutputs = coerceMap(s.PriorOutputs)
		s.ObservedOutputs = coerceMap(s.ObservedOutputs)
	}
	return &pf, nil
}

// coerceNumbers walks a decoded JSON tree and replaces every
// json.Number with int64 (when the number has no decimal point or
// exponent) or float64 (otherwise). Maps and slices recurse.
func coerceNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			if i, err := x.Int64(); err == nil {
				return i
			}
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return s
	case map[string]any:
		return coerceMap(x)
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = coerceNumbers(el)
		}
		return out
	default:
		return v
	}
}

func coerceMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = coerceNumbers(v)
	}
	return out
}
