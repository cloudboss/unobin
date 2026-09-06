package e2etest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/encrypters"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

func seedState(workspace string, c CompiledCase) error {
	if c.StateSeed == "" {
		return nil
	}
	path := filepath.Join(c.Dir, filepath.FromSlash(c.StateSeed))
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read state seed %s: %w", c.StateSeed, err)
	}
	snap, err := sdkstate.DecodeSnapshotV2(body)
	if err != nil {
		return fmt.Errorf("decode state seed %s: %w", c.StateSeed, err)
	}

	store, err := local.NewStore(
		filepath.Join(workspace, ".unobin", "state"),
		c.Name,
		snap.Stack,
		encrypters.Noop{},
	)
	if err != nil {
		return err
	}
	rev, err := store.WriteV2(&snap)
	if err != nil {
		return fmt.Errorf("write state seed %s: %w", c.StateSeed, err)
	}
	if err := store.SetCurrent(rev); err != nil {
		return fmt.Errorf("set current state seed %s: %w", c.StateSeed, err)
	}
	for range c.ExtraStateSnapshots {
		extra, err := sdkstate.NewSnapshotV2(snap.Factory, snap.Stack)
		if err != nil {
			return err
		}
		if _, err := store.WriteV2(extra); err != nil {
			return fmt.Errorf("write extra state seed %s: %w", c.StateSeed, err)
		}
	}
	return nil
}

func createStateLocks(workspace string, c CompiledCase) error {
	for _, stack := range c.StateLocks {
		path := filepath.Join(
			workspace,
			".unobin",
			"state",
			c.Name,
			filepath.FromSlash(stack),
			"lock",
		)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("make state lock directory: %w", err)
		}
		if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
			return fmt.Errorf("write state lock %s: %w", stack, err)
		}
	}
	return nil
}
