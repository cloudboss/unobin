package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// PlanFormatVersion is the schema version this package reads and writes
// for plan files.
const PlanFormatVersion = 1

// PlanFile is the on-disk shape of a plan. Steps in the file mirror
// the in-memory `Plan.Steps` but use `json` tags for stable
// serialization. Inputs carries the validated root inputs the plan was
// computed against, so apply can seed them into its eval scope without
// re-reading the stack file.
type PlanFile struct {
	FormatVersion  int                 `json:"format-version"`
	Factory        FactoryRef          `json:"factory"`
	Stack          string              `json:"stack"`
	StateRev       string              `json:"state-rev"`
	GeneratedAt    time.Time           `json:"generated-at"`
	Inputs         map[string]any      `json:"inputs,omitempty"`
	Configurations *PlanConfigurations `json:"configurations,omitempty"`

	RawConfigurations map[string]map[string]any `json:"-"`

	Backend     *StateRef  `json:"backend,omitempty"`
	Parallelism int        `json:"parallelism,omitempty"`
	Destroy     bool       `json:"destroy,omitempty"`
	Steps       []PlanStep `json:"steps"`
}

// PlanConfigurations is the generated plan-file form of stack
// configuration values.
type PlanConfigurations struct {
	Named    map[string]PlanNamedConfiguration   `json:"named,omitempty"`
	Defaults map[string]PlanDefaultConfiguration `json:"defaults,omitempty"`
}

type PlanNamedConfiguration struct {
	Selector state.Selector `json:"selector"`
	Body     map[string]any `json:"body"`
}

type PlanDefaultConfiguration struct {
	Body map[string]any `json:"body"`
}

type planStepJSON struct {
	Address          string                  `json:"address"`
	Kind             NodeKind                `json:"node-kind"`
	Selector         *state.Selector         `json:"selector,omitempty"`
	Composite        bool                    `json:"composite,omitempty"`
	Decision         Decision                `json:"decision"`
	Inputs           map[string]any          `json:"inputs,omitempty"`
	UnresolvedInputs map[string][]string     `json:"unresolved-inputs,omitempty"`
	DeferredRead     *state.ConfigurationRef `json:"deferred-read,omitempty"`
	PriorInputs      map[string]any          `json:"prior-inputs,omitempty"`
	PriorSelector    *state.Selector         `json:"prior-selector,omitempty"`
	PriorOutputs     map[string]any          `json:"prior-outputs,omitempty"`
	ObservedOutputs  map[string]any          `json:"observed-outputs,omitempty"`
	TriggerHash      string                  `json:"trigger-hash,omitempty"`
	ReplaceTriggers  []string                `json:"replace-triggers,omitempty"`
	Configuration    *state.ConfigurationRef `json:"configuration,omitempty"`
	DependsOn        []string                `json:"depends-on,omitempty"`
	AlreadyGone      bool                    `json:"already-gone,omitempty"`
	SensitiveInputs  []string                `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string                `json:"sensitive-outputs,omitempty"`
}

func (s PlanStep) MarshalJSON() ([]byte, error) {
	cfg, err := state.EncodeConfigurationRef(s.Configuration)
	if err != nil {
		return nil, err
	}
	deferredRead, err := state.EncodeConfigurationRef(s.DeferredRead)
	if err != nil {
		return nil, err
	}
	return json.Marshal(planStepJSON{
		Address:          s.Address,
		Kind:             s.Kind,
		Selector:         s.Selector,
		Composite:        s.Composite,
		Decision:         s.Decision,
		Inputs:           s.Inputs,
		UnresolvedInputs: s.UnresolvedInputs,
		DeferredRead:     deferredRead,
		PriorInputs:      s.PriorInputs,
		PriorSelector:    s.PriorSelector,
		PriorOutputs:     s.PriorOutputs,
		ObservedOutputs:  s.ObservedOutputs,
		TriggerHash:      s.TriggerHash,
		ReplaceTriggers:  s.ReplaceTriggers,
		Configuration:    cfg,
		DependsOn:        s.DependsOn,
		AlreadyGone:      s.AlreadyGone,
		SensitiveInputs:  s.SensitiveInputs,
		SensitiveOutputs: s.SensitiveOutputs,
	})
}

func (s *PlanStep) UnmarshalJSON(b []byte) error {
	var raw struct {
		planStepJSON
		Configuration json.RawMessage `json:"configuration"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	cfg, err := state.DecodeConfigurationRefJSON(raw.Configuration)
	if err != nil {
		return err
	}
	deferredRead, err := state.DecodeConfigurationRef(raw.DeferredRead)
	if err != nil {
		return err
	}
	*s = PlanStep{
		Address:          raw.Address,
		Kind:             raw.Kind,
		Selector:         raw.Selector,
		Composite:        raw.Composite,
		Decision:         raw.Decision,
		Inputs:           raw.Inputs,
		UnresolvedInputs: raw.UnresolvedInputs,
		DeferredRead:     deferredRead,
		PriorInputs:      raw.PriorInputs,
		PriorSelector:    raw.PriorSelector,
		PriorOutputs:     raw.PriorOutputs,
		ObservedOutputs:  raw.ObservedOutputs,
		TriggerHash:      raw.TriggerHash,
		ReplaceTriggers:  raw.ReplaceTriggers,
		Configuration:    cfg,
		DependsOn:        raw.DependsOn,
		AlreadyGone:      raw.AlreadyGone,
		SensitiveInputs:  raw.SensitiveInputs,
		SensitiveOutputs: raw.SensitiveOutputs,
	}
	return nil
}

func (c *PlanConfigurations) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if bytes.Equal(trimmed, []byte("null")) {
		*c = PlanConfigurations{}
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return err
	}
	for name := range fields {
		switch name {
		case "named", "defaults":
		default:
			return fmt.Errorf("configurations: unknown field %q", name)
		}
	}
	type plain PlanConfigurations
	var out plain
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return err
	}
	*c = PlanConfigurations(out)
	return nil
}

// FactoryRef identifies the stack a plan was computed against.
type FactoryRef struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}

func encodePlanConfigurations(
	raw map[string]map[string]any,
) (*PlanConfigurations, error) {
	out := &PlanConfigurations{}
	var added bool
	for alias, names := range raw {
		if alias == "" {
			return nil, fmt.Errorf("plan: configuration selector is empty")
		}
		for name, bodyValue := range names {
			if name == "" {
				return nil, fmt.Errorf("plan: configuration name is empty")
			}
			body, ok := bodyValue.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(
					"plan: configuration %s.%s body must be an object", alias, name)
			}
			if name == "default" {
				if out.Defaults == nil {
					out.Defaults = map[string]PlanDefaultConfiguration{}
				}
				out.Defaults[alias] = PlanDefaultConfiguration{Body: body}
				added = true
				continue
			}
			if out.Named == nil {
				out.Named = map[string]PlanNamedConfiguration{}
			}
			if _, exists := out.Named[name]; exists {
				return nil, fmt.Errorf("plan: duplicate named configuration %q", name)
			}
			out.Named[name] = PlanNamedConfiguration{
				Selector: state.Selector{Alias: alias},
				Body:     body,
			}
			added = true
		}
	}
	if !added {
		return nil, nil
	}
	return out, nil
}

func decodePlanConfigurations(
	configurations *PlanConfigurations,
) (map[string]map[string]any, error) {
	if configurations == nil {
		return nil, nil
	}
	out := map[string]map[string]any{}
	for alias, cfg := range configurations.Defaults {
		if alias == "" {
			return nil, fmt.Errorf("plan: default configuration selector is empty")
		}
		body, err := planConfigurationBody(alias+".default", cfg.Body)
		if err != nil {
			return nil, err
		}
		setPlanConfiguration(out, alias, "default", body)
	}
	for name, cfg := range configurations.Named {
		if name == "" {
			return nil, fmt.Errorf("plan: named configuration name is empty")
		}
		if cfg.Selector.Alias == "" {
			return nil, fmt.Errorf("plan: named configuration %s has no selector", name)
		}
		if cfg.Selector.Export != "" {
			return nil, fmt.Errorf(
				"plan: named configuration %s selector must have only alias", name)
		}
		body, err := planConfigurationBody(name, cfg.Body)
		if err != nil {
			return nil, err
		}
		setPlanConfiguration(out, cfg.Selector.Alias, name, body)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func planConfigurationBody(label string, body map[string]any) (map[string]any, error) {
	if body == nil {
		return nil, fmt.Errorf("plan: configuration %s missing body", label)
	}
	return coerceMap(body), nil
}

func setPlanConfiguration(
	configs map[string]map[string]any,
	alias string,
	name string,
	body map[string]any,
) {
	if configs[alias] == nil {
		configs[alias] = map[string]any{}
	}
	configs[alias][name] = body
}

// EncodePlan renders a plan as JSON bytes for on-disk storage.
func EncodePlan(p *Plan) ([]byte, error) {
	steps := make([]PlanStep, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = *s
	}
	configurations, err := encodePlanConfigurations(p.RawConfigurations)
	if err != nil {
		return nil, err
	}
	pf := PlanFile{
		FormatVersion: PlanFormatVersion,
		Factory: FactoryRef{
			Name:            p.Factory.Name,
			Version:         p.Factory.Version,
			ContentRevision: p.Factory.ContentRevision,
		},
		Stack:          p.Stack,
		StateRev:       p.StateRev,
		GeneratedAt:    time.Now().UTC(),
		Inputs:         p.Inputs,
		Configurations: configurations,
		Backend:        p.Backend,
		Parallelism:    p.Parallelism,
		Destroy:        p.Destroy,
		Steps:          steps,
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
	rawConfigurations, err := decodePlanConfigurations(pf.Configurations)
	if err != nil {
		return nil, err
	}
	pf.RawConfigurations = rawConfigurations
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
