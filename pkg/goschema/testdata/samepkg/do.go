package samepkg

import (
	"context"
	"time"
)

type DoAction struct {
	What string `mapstructure:"what"`
}

type DoActionOutput struct {
	Result   string        `mapstructure:"result"`
	Duration time.Duration `mapstructure:"duration"`
	Tags     []string      `mapstructure:"tags"`
}

func (a *DoAction) Run(ctx context.Context, _ any) (any, error) {
	return DoActionOutput{}, nil
}

type Do2Action struct{}

// Do2ActionOutput follows the same shape as DoActionOutput; the
// alias keeps the GoName + Output convention without duplicating
// the field list.
type Do2ActionOutput = DoActionOutput

func (a *Do2Action) Run(ctx context.Context, _ any) (any, error) {
	return Do2ActionOutput{}, nil
}
