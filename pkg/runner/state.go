package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/state"
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
	cmd.AddCommand(newStateGCCmd(info))
	cmd.AddCommand(newStateForceUnlockCmd(info))
	return cmd
}

// addConfigFlag attaches a -c flag to a state subcommand. The flag is
// the only way to select which deployment the command operates on
// once deployment id comes from the config filename.
func addConfigFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVarP(dst, "config", "c", "",
		"Path to a config.ub identifying the deployment.")
}

func newStateGCCmd(info Info) *cobra.Command {
	var (
		keep       int
		configPath string
	)
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Delete old snapshot revisions, keeping the most recent ones",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateGC(cmd, info, configPath, keep)
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 10,
		"Number of recent snapshot revisions to keep. The current revision"+
			" is always kept in addition to these.")
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateGC(cmd *cobra.Command, info Info, configPath string, keep int) error {
	if keep < 0 {
		return fmt.Errorf("--keep must not be negative")
	}
	config, err := parseConfigFile(configPath)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
	if err != nil {
		return err
	}
	lock, err := store.Lock(context.Background())
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	revs, err := store.List()
	if err != nil {
		return err
	}
	current, err := store.CurrentRev()
	if err != nil && !errors.Is(err, state.ErrNoCurrent) {
		return err
	}

	keepSet := map[string]bool{}
	if current != "" {
		keepSet[current] = true
	}
	cutoff := max(len(revs)-keep, 0)
	for _, r := range revs[cutoff:] {
		keepSet[r] = true
	}

	var deleted int
	for _, r := range revs {
		if keepSet[r] {
			continue
		}
		if err := store.Delete(r); err != nil {
			return err
		}
		deleted++
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d snapshot(s), kept %d.\n",
		deleted, len(revs)-deleted)
	return nil
}

func newStateMoveCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "move <old-address> <new-address>",
		Short: "Move a state entry to a new address",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateMove(cmd, info, configPath, args[0], args[1])
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateMove(cmd *cobra.Command, info Info, configPath, oldAddr, newAddr string) error {
	if oldAddr == newAddr {
		return fmt.Errorf("old and new address are the same")
	}
	config, err := parseConfigFile(configPath)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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

	var moved []*state.Entry
	occupied := map[string]bool{}
	for _, e := range snap.Entries {
		if e.Address == oldAddr || strings.HasPrefix(e.Address, oldAddr+"/") {
			moved = append(moved, e)
		} else {
			occupied[e.Address] = true
		}
	}
	if len(moved) == 0 {
		return fmt.Errorf("no entry at %s", oldAddr)
	}
	for _, e := range moved {
		target := newAddr + e.Address[len(oldAddr):]
		if occupied[target] {
			return fmt.Errorf("an entry already exists at %s", target)
		}
		occupied[target] = true
	}
	for _, e := range moved {
		e.Address = newAddr + e.Address[len(oldAddr):]
	}

	rev, err := store.Write(snap)
	if err != nil {
		return err
	}
	if err := store.SetCurrent(rev); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(moved) == 1 {
		fmt.Fprintf(out, "Moved %s to %s.\n", oldAddr, newAddr)
	} else {
		fmt.Fprintf(out, "Moved %s to %s (%d entries).\n", oldAddr, newAddr, len(moved))
	}
	return nil
}

func newStateRemoveCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove <address>",
		Short: "Remove a state entry without touching the underlying resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateRemove(cmd, info, configPath, args[0])
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateRemove(cmd *cobra.Command, info Info, configPath, addr string) error {
	config, err := parseConfigFile(configPath)
	if err != nil {
		return err
	}
	enc, err := loadEncrypter(info, config, configPath)
	if err != nil {
		return err
	}
	store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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
	var configPath string
	cmd := &cobra.Command{
		Use:   "force-unlock",
		Short: "Remove the deployment's lock without checking who holds it",
		Long: "Use this only when a previous run died without releasing the lock. " +
			"Make sure no apply or refresh is running against this deployment first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			enc, err := loadEncrypter(info, config, configPath)
			if err != nil {
				return err
			}
			store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateListCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshot revisions, marking the current one with *",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			enc, err := loadEncrypter(info, config, configPath)
			if err != nil {
				return err
			}
			store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateShowCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "show [revision]",
		Short: "Show a snapshot's entries (current snapshot if no revision given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseConfigFile(configPath)
			if err != nil {
				return err
			}
			enc, err := loadEncrypter(info, config, configPath)
			if err != nil {
				return err
			}
			store, err := loadStore(info, config, configPath, stackName(configPath), enc)
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
			source, err := parsedFile(info)
			if err != nil {
				return err
			}
			return printSnapshot(cmd, snap, rootSensitiveOutputs(source))
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func printSnapshot(cmd *cobra.Command, snap *state.Snapshot, sensitive map[string]bool) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "factory:      %s %s (content-revision %s)\n",
		snap.Factory.Name, snap.Factory.Version, snap.Factory.ContentRevision)
	fmt.Fprintf(out, "stack:        %s\n", snap.Stack)
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
			value := sensitivePlaceholder
			if !sensitive[k] {
				value = strings.ReplaceAll(lang.RenderPretty(snap.Outputs[k]), "\n", "\n  ")
			}
			fmt.Fprintf(out, "  %s: %s\n", k, value)
		}
	}
	return nil
}
