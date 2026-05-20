// Package greet is a small demonstration module that exists so
// examples and tests can exercise configuration routing. The module
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

// SayAction prepends the configured prefix to a message.
type SayAction struct {
	Message string `mapstructure:"message"`
}

// SayActionOutput is the action's output.
type SayActionOutput struct {
	Output string `mapstructure:"output"`
}

func (a *SayAction) Run(_ context.Context, rawCfg any) (*SayActionOutput, error) {
	c, ok := rawCfg.(*Configuration)
	if !ok {
		return nil, fmt.Errorf("greet.say: missing or wrong configuration (got %T)", rawCfg)
	}
	return &SayActionOutput{Output: c.Prefix.Value + ": " + a.Message}, nil
}

// Module returns the registration record for the `greet` module.
func Module() *runtime.Module {
	return &runtime.Module{
		Name:        "greet",
		Description: "Demonstrates configuration routing by prepending a prefix to a message.",
		Configuration: &cfg.ConfigurationType{
			Description: "Greeting prefix.",
			New:         func() any { return &Configuration{} },
		},
		Actions: map[string]runtime.ActionRegistration{
			"say": runtime.MakeAction[SayAction, *SayActionOutput](),
		},
	}
}
