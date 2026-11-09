package statechecks

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/encrypters"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type RejectStack struct {
	Path string
}

type StackResult struct {
	Rejected bool
}

func (r *RejectStack) Run(ctx context.Context, _ ubruntime.NoConfig) (*StackResult, error) {
	const name = "sdk-stack"
	source, err := local.NewStore(r.Path, name, "source", encrypters.Noop{})
	if err != nil {
		return nil, err
	}
	target, err := local.NewStore(r.Path, name, "target", encrypters.Noop{})
	if err != nil {
		return nil, err
	}
	executor := &ubruntime.Executor{
		DAG: &ubruntime.DAG{Nodes: map[string]*ubruntime.Node{}}, Store: source,
		Factory: state.FactoryInfo{Name: name, Version: "v0.0.0", ContentRevision: "test"},
	}
	plan, err := executor.Plan(ctx)
	if err != nil {
		return nil, err
	}
	sealed, err := ubruntime.SealPlan(plan, encrypters.Noop{})
	if err != nil {
		return nil, err
	}
	saved, err := ubruntime.OpenPlan(sealed, func(*ubruntime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return nil, err
	}
	executor.Store = target
	_, err = executor.ApplyPlan(ctx, saved)
	if err == nil {
		return nil, errors.New("plan for another stack was accepted")
	}
	const expected = `plan was computed for stack "source", this backend is stack "target"`
	if err.Error() != expected {
		return nil, fmt.Errorf("unexpected apply error: %w", err)
	}
	_, err = target.CurrentRev()
	if !errors.Is(err, state.ErrNoCurrent) {
		return nil, errors.New("rejected plan changed the target stack")
	}
	revisions, err := target.List()
	if err != nil {
		return nil, err
	}
	if len(revisions) != 0 {
		return nil, errors.New("rejected plan wrote target snapshots")
	}
	return &StackResult{Rejected: true}, nil
}
