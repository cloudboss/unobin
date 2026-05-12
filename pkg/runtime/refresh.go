package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/state"
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
	for _, ent := range rs.prior.Entries {
		if ent.Type != state.EntryLeaf {
			rs.next.Entries = append(rs.next.Entries, ent)
			continue
		}
		updated, dropped, err := refreshLeaf(ctx, e.modulesForAddress(ent.Address), ent)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ent.Address, err)
		}
		if dropped {
			res.Dropped++
			continue
		}
		rs.next.Entries = append(rs.next.Entries, updated)
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

func refreshLeaf(
	ctx context.Context,
	modules map[string]*Module,
	ent *state.Entry,
) (*state.Entry, bool, error) {
	ns, typeName, _, ok := parseResourceAddress(innerAddress(ent.Address))
	if !ok {
		return nil, false, fmt.Errorf("malformed resource address %q", ent.Address)
	}
	mod, ok := modules[ns]
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
	observed, err := readObserved(ctx, rt, ent.Inputs, priorOutputs)
	if errors.Is(err, ErrNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &state.Entry{
		Address:         ent.Address,
		Type:            state.EntryLeaf,
		Kind:            ent.Kind,
		SchemaVersion:   rt.SchemaVersion,
		SensitiveFields: ent.SensitiveFields,
		TriggerHash:     ent.TriggerHash,
		Inputs:          ent.Inputs,
		Outputs:         observed,
		DependsOn:       ent.DependsOn,
	}, false, nil
}
