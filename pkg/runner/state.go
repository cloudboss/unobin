package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/pkg/state"
	"github.com/spf13/cobra"
)

func newStateCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Inspect the deployment's state",
	}
	cmd.AddCommand(newStateListCmd(info))
	cmd.AddCommand(newStateShowCmd(info))
	cmd.AddCommand(newStateMoveCmd(info))
	cmd.AddCommand(newStateRemoveCmd(info))
	cmd.AddCommand(newStateForceUnlockCmd(info))
	return cmd
}

func newStateMoveCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "move <old-address> <new-address>",
		Short: "Move a state entry to a new address",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateMove(cmd, info, args[0], args[1])
		},
	}
}

func doStateMove(cmd *cobra.Command, info Info, oldAddr, newAddr string) error {
	if oldAddr == newAddr {
		return fmt.Errorf("old and new address are the same")
	}
	enc, err := loadEncrypter()
	if err != nil {
		return err
	}
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	lock, err := store.Lock(context.Background())
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	snap, err := store.Current()
	if err != nil {
		return err
	}
	if snap.Find(newAddr) != nil {
		return fmt.Errorf("an entry already exists at %s", newAddr)
	}
	entry := snap.Find(oldAddr)
	if entry == nil {
		return fmt.Errorf("no entry at %s", oldAddr)
	}
	entry.Address = newAddr

	rev, err := store.Write(snap)
	if err != nil {
		return err
	}
	if err := store.SetCurrent(rev); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Moved %s to %s.\n", oldAddr, newAddr)
	return nil
}

func newStateRemoveCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <address>",
		Short: "Remove a state entry without touching the underlying resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateRemove(cmd, info, args[0])
		},
	}
}

func doStateRemove(cmd *cobra.Command, info Info, addr string) error {
	enc, err := loadEncrypter()
	if err != nil {
		return err
	}
	store, err := loadStore(info, enc)
	if err != nil {
		return err
	}
	lock, err := store.Lock(context.Background())
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	snap, err := store.Current()
	if err != nil {
		return err
	}
	idx := -1
	for i, e := range snap.Entries {
		if e.Address == addr {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no entry at %s", addr)
	}
	snap.Entries = append(snap.Entries[:idx], snap.Entries[idx+1:]...)

	rev, err := store.Write(snap)
	if err != nil {
		return err
	}
	if err := store.SetCurrent(rev); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", addr)
	return nil
}

func newStateForceUnlockCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "force-unlock",
		Short: "Remove the deployment's lock without checking who holds it",
		Long: "Use this only when a previous run died without releasing the lock. " +
			"Make sure no apply or refresh is running against this deployment first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := loadEncrypter()
			if err != nil {
				return err
			}
			store, err := loadStore(info, enc)
			if err != nil {
				return err
			}
			if err := store.ForceUnlock(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Lock cleared.")
			return nil
		},
	}
}

func newStateListCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List snapshot revisions, marking the current one with *",
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := loadEncrypter()
			if err != nil {
				return err
			}
			store, err := loadStore(info, enc)
			if err != nil {
				return err
			}
			revs, err := store.List()
			if err != nil {
				return err
			}
			current, _ := store.CurrentRev()
			out := cmd.OutOrStdout()
			for _, r := range revs {
				marker := "  "
				if r == current {
					marker = "* "
				}
				fmt.Fprintf(out, "%s%s\n", marker, r)
			}
			return nil
		},
	}
}

func newStateShowCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "show [revision]",
		Short: "Show a snapshot's entries (current snapshot if no revision given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := loadEncrypter()
			if err != nil {
				return err
			}
			store, err := loadStore(info, enc)
			if err != nil {
				return err
			}
			var snap *state.Snapshot
			if len(args) == 0 {
				snap, err = store.Current()
			} else {
				snap, err = store.Get(args[0])
			}
			if err != nil {
				return err
			}
			return printSnapshot(cmd, snap)
		},
	}
}

func printSnapshot(cmd *cobra.Command, snap *state.Snapshot) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "stack:        %s %s (commit %s)\n",
		snap.Stack.Name, snap.Stack.Version, snap.Stack.Commit)
	fmt.Fprintf(out, "deployment:   %s\n", snap.DeploymentID)
	fmt.Fprintf(out, "generated-at: %s\n", snap.GeneratedAt.Format("2006-01-02 15:04:05Z07:00"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "entries:")
	for _, e := range snap.Entries {
		fmt.Fprintf(out, "  %s [%s]\n", e.Address, e.Type)
	}
	if len(snap.Outputs) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "outputs:")
		for _, k := range sortedMapKeys(snap.Outputs) {
			b, _ := json.Marshal(snap.Outputs[k])
			fmt.Fprintf(out, "  %s = %s\n", k, string(b))
		}
	}
	return nil
}
