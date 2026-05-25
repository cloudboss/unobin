package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// RefreshResult reports what Refresh did. Refreshed counts leaf
// entries whose outputs were updated to match what was observed;
// Dropped counts leaves whose Read returned ErrNotFound and were
// removed from state.
type RefreshResult struct {
	WrittenRev string
	Refreshed  int
	Dropped    int
}

// Refresh reads every resource recorded in prior state and writes a
// fresh snapshot whose leaf outputs reflect the observation. Resources
// that are no longer present are dropped. Action and module-call
// entries, plus stack-level outputs, carry forward unchanged. No
// resource writes happen. The deployment's lock is held for the
// duration.
func (e *Executor) Refresh(ctx context.Context) (*RefreshResult, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if err := e.checkConfigurations(); err != nil {
		return nil, err
	}
	lock, err := e.Store.Lock(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	rs, err := e.initRun()
	if err != nil {
		return nil, err
	}
	if rs.prior == nil {
		return &RefreshResult{}, nil
	}

	res := &RefreshResult{}
	type leafResult struct {
		idx     int
		updated *state.Entry
		dropped bool
		err     error
	}
	leaves := []*state.Entry{}
	carry := []*state.Entry{}
	for _, ent := range rs.prior.Entries {
		if ent.Type != state.EntryLeaf {
			carry = append(carry, ent)
			continue
		}
		leaves = append(leaves, ent)
	}
	results := make([]leafResult, len(leaves))
	sem := make(chan struct{}, e.effectiveParallelism())
	var wg sync.WaitGroup
	for i, ent := range leaves {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ent *state.Entry) {
			defer func() { <-sem; wg.Done() }()
			updated, dropped, err := e.refreshLeaf(ctx, ent)
			results[i] = leafResult{idx: i, updated: updated, dropped: dropped, err: err}
		}(i, ent)
	}
	wg.Wait()
	rs.next.Entries = append(rs.next.Entries, carry...)
	for _, r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("%s: %w", leaves[r.idx].Address, r.err)
		}
		if r.dropped {
			res.Dropped++
			continue
		}
		rs.next.Entries = append(rs.next.Entries, r.updated)
		res.Refreshed++
	}
	rs.next.Outputs = rs.prior.Outputs

	rev, err := e.persist(rs)
	if err != nil {
		return nil, err
	}
	res.WrittenRev = rev
	return res, nil
}

func (e *Executor) refreshLeaf(
	ctx context.Context,
	ent *state.Entry,
) (*state.Entry, bool, error) {
	ns, typeName, _, ok := parseResourceAddress(innerAddress(ent.Address))
	if !ok {
		return nil, false, fmt.Errorf("malformed resource address %q", ent.Address)
	}
	mod, ok := e.modulesForAddress(ent.Address)[ns]
	if !ok {
		return nil, false, fmt.Errorf("module %q is not imported", ns)
	}
	rt, ok := mod.Resources[typeName]
	if !ok {
		return nil, false, fmt.Errorf("module %s has no resource %q", ns, typeName)
	}
	priorOutputs, err := migrateOutputs(rt, ent.SchemaVersion, ent.Outputs)
	if err != nil {
		return nil, false, err
	}
	observed, err := readObserved(ctx, rt,
		e.configForRef(ent.Configuration, ns), ent.Inputs, priorOutputs)
	if errors.Is(err, ErrNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &state.Entry{
		Address:          ent.Address,
		Type:             state.EntryLeaf,
		Kind:             ent.Kind,
		SchemaVersion:    rt.SchemaVersion(),
		Configuration:    ent.Configuration,
		SensitiveInputs:  ent.SensitiveInputs,
		SensitiveOutputs: ent.SensitiveOutputs,
		TriggerHash:      ent.TriggerHash,
		Inputs:           ent.Inputs,
		Outputs:          observed,
		DependsOn:        ent.DependsOn,
	}, false, nil
}
