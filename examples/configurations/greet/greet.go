// Package greet is a small demonstration library that exists so
// examples and tests can exercise configuration routing. The library
// carries a single configuration field (Prefix) and a single action
// (say) that prepends the prefix to a caller-supplied message.
package greet

import (
	"context"
	"fmt"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// Configuration is the per-alias body operators fill out in
// `configurations.greet.<alias>` in config.ub.
type Configuration struct {
	Prefix cfg.String
}

// PhraseResource stores a phrase and computes a decorated form of it,
// the value the example's internal configuration derives a prefix
// from.
type PhraseResource struct {
	Text string
}

// PhraseResourceOutput is the resource's stored result.
type PhraseResourceOutput struct {
	Text      string
	Decorated string
}

func (r *PhraseResource) SchemaVersion() int { return 1 }

func (r *PhraseResource) Create(_ context.Context, _ any) (*PhraseResourceOutput, error) {
	return &PhraseResourceOutput{Text: r.Text, Decorated: "** " + r.Text + " **"}, nil
}

func (r *PhraseResource) Read(
	_ context.Context, _ any, prior *PhraseResourceOutput,
) (*PhraseResourceOutput, error) {
	return prior, nil
}

func (r *PhraseResource) Update(
	_ context.Context, _ any, _ runtime.Prior[PhraseResource, *PhraseResourceOutput],
) (*PhraseResourceOutput, error) {
	return &PhraseResourceOutput{Text: r.Text, Decorated: "** " + r.Text + " **"}, nil
}

func (r *PhraseResource) Delete(_ context.Context, _ any, _ *PhraseResourceOutput) error {
	return nil
}

func (r *PhraseResource) ReplaceFields() []string { return nil }

// SayAction prepends the configured prefix to a message.
type SayAction struct {
	Message string
}

// SayActionOutput is the action's output.
type SayActionOutput struct {
	Output string
}

func (a *SayAction) Run(_ context.Context, rawCfg any) (*SayActionOutput, error) {
	c, ok := rawCfg.(*Configuration)
	if !ok {
		return nil, fmt.Errorf("greet.say: missing or wrong configuration (got %T)", rawCfg)
	}
	return &SayActionOutput{Output: c.Prefix.Value + ": " + a.Message}, nil
}

// Library returns the registration record for the `greet` library.
func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "greet",
		Description: "Demonstrates configuration routing by prepending a prefix to a message.",
		Configuration: &cfg.ConfigurationType{
			Description: "Greeting prefix.",
			New:         func() any { return &Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"phrase": runtime.MakeResource[PhraseResource, *PhraseResourceOutput](),
		},
		Actions: map[string]runtime.ActionRegistration{
			"say": runtime.MakeAction[SayAction, *SayActionOutput](),
		},
	}
}
