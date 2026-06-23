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
// that are no longer present are dropped. Action and library-call
// entries, plus stack-level outputs, carry forward unchanged. No
// resource writes happen. The stack's lock is held for the
// duration.
func (e *Executor) Refresh(ctx context.Context) (*RefreshResult, error) {
	if e.Store == nil {
		return nil, errors.New("executor: Store is required")
	}
	if err := e.CheckLibraryConfigs(); err != nil {
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
	if err := e.seedPriorInternalConfigurations(rs.prior, e.Inputs); err != nil {
		return nil, err
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
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			var dropped bool
			updated, err := guard("refreshing this resource", true, func() (*state.Entry, error) {
				u, d, rerr := e.refreshLeaf(ctx, ent)
				dropped = d
				return u, rerr
			})
			results[i] = leafResult{idx: i, updated: updated, dropped: dropped, err: err}
		})
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
	alias, typeName, ok := entryBindingParts(ent)
	if !ok {
		return nil, false, fmt.Errorf("missing binding for resource %q", ent.Address)
	}
	lib, ok := e.librariesForAddress(ent.Address)[alias]
	if !ok {
		return nil, false, fmt.Errorf("library %q is not imported", alias)
	}
	rt, ok := lib.Resources[typeName]
	if !ok {
		return nil, false, fmt.Errorf("library %s has no resource %q", alias, typeName)
	}
	migrated, err := migrateEntry(rt, alias, ent.SchemaVersion,
		MigrationState{Inputs: ent.Inputs, Outputs: ent.Outputs})
	if err != nil {
		return nil, false, err
	}
	cfg, err := e.configForStateAddress(ent.Address, alias)
	if err != nil {
		return nil, false, err
	}
	observed, err := readObserved(ctx, rt, alias,
		cfg, migrated.Inputs, migrated.Outputs)
	if errors.Is(err, ErrNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &state.Entry{
		Address:          ent.Address,
		Type:             state.EntryLeaf,
		Category:         string(NodeResource),
		Binding:          bindingFromEntry(ent),
		SchemaVersion:    rt.SchemaVersion(),
		SensitiveInputs:  ent.SensitiveInputs,
		SensitiveOutputs: ent.SensitiveOutputs,
		TriggerHash:      ent.TriggerHash,
		Inputs:           migrated.Inputs,
		Outputs:          observed,
		DependsOn:        ent.DependsOn,
	}, false, nil
}
