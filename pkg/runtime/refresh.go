package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// RefreshResult reports observed resources, removed resources, and the saved revision.
type RefreshResult struct {
	WrittenRev string
	Refreshed  int
	Dropped    int
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
	observed, err := e.readObserved(ctx, rt, alias,
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
