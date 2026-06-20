package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/spf13/cobra"
)

func newStateCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Inspect the stack's state",
	}
	cmd.AddCommand(newStateListCmd(info))
	cmd.AddCommand(newStateShowCmd(info))
	cmd.AddCommand(newStatePullCmd(info))
	cmd.AddCommand(newStateMoveCmd(info))
	cmd.AddCommand(newStateRemoveCmd(info))
	cmd.AddCommand(newStateSnapshotsCmd(info))
	cmd.AddCommand(newStateForceUnlockCmd(info))
	return cmd
}

// addConfigFlag attaches a -c flag to a state subcommand. The flag is
// the only way to select which stack the command operates on
// once stack name comes from the stack file name.
func addConfigFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVarP(dst, "config", "c", "",
		"Path to a stack file identifying the stack.")
}

func newStateSnapshotsCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Inspect and delete snapshot revisions",
	}
	cmd.AddCommand(newStateSnapshotsListCmd(info))
	cmd.AddCommand(newStateGCCmd(info))
	return cmd
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
	store, err := loadStateStore(info, configPath)
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
		Use:   "move <from-selector@from-address> <to-selector@to-address>",
		Short: "Move a state entry to a new address or selector",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateMove(cmd, info, configPath, args[0], args[1])
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateMove(cmd *cobra.Command, info Info, configPath, fromText, toText string) error {
	from, err := runtime.ParseEntryRef(fromText)
	if err != nil {
		return fmt.Errorf("state move: %w", err)
	}
	to, err := runtime.ParseEntryRef(toText)
	if err != nil {
		return fmt.Errorf("state move: %w", err)
	}
	parsed, err := parseFactory(info)
	if err != nil {
		return err
	}
	store, err := loadStateStore(info, configPath)
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
	next, moved, err := runtime.ApplyEntryMoves(
		snap,
		parsed.dag,
		info.Libraries,
		[]runtime.EntryMoveSpec{{From: from, To: to}},
		runtime.EntryMoveStrict,
	)
	if err != nil {
		return err
	}

	rev, err := store.Write(next)
	if err != nil {
		return err
	}
	if err := store.SetCurrent(rev); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(moved) == 1 {
		fmt.Fprintf(out, "Moved %s to %s.\n", from.String(), to.String())
	} else {
		fmt.Fprintf(out, "Moved %s to %s (%d entries).\n",
			from.String(), to.String(), len(moved))
	}
	return nil
}

func newStateRemoveCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove <selector@address>",
		Short: "Remove a state entry without touching the underlying resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doStateRemove(cmd, info, configPath, args[0])
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateRemove(cmd *cobra.Command, info Info, configPath, refText string) error {
	ref, err := runtime.ParseEntryRef(refText)
	if err != nil {
		return fmt.Errorf("state remove: %w", err)
	}
	store, err := loadStateStore(info, configPath)
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
		entryRef, ok := runtime.EntryRefFromEntry(e)
		if ok && runtime.SameEntryRef(entryRef, ref) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no entry at %s", ref.String())
	}
	snap.Entries = append(snap.Entries[:idx], snap.Entries[idx+1:]...)

	rev, err := store.Write(snap)
	if err != nil {
		return err
	}
	if err := store.SetCurrent(rev); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", ref.String())
	return nil
}

func newStateForceUnlockCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "force-unlock",
		Short: "Remove the stack's lock without checking who holds it",
		Long: "Use this only when a previous run died without releasing the lock. " +
			"Make sure no apply or refresh is running against this stack first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadStateStore(info, configPath)
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
		Short: "List current state entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadStateStore(info, configPath)
			if err != nil {
				return err
			}
			snap, err := store.Current()
			if errors.Is(err, state.ErrNoCurrent) {
				return nil
			}
			if err != nil {
				return err
			}
			refs, err := stateEntryRefs(snap)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, ref := range refs {
				fmt.Fprintln(out, ref)
			}
			return nil
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateSnapshotsListCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshot revisions, marking the current one with *",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadStateStore(info, configPath)
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
		Use:   "show <selector@address>",
		Short: "Show one current state entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := runtime.ParseEntryRef(args[0])
			if err != nil {
				return fmt.Errorf("state show: %w", err)
			}
			store, err := loadStateStore(info, configPath)
			if err != nil {
				return err
			}
			snap, err := store.Current()
			if err != nil {
				return err
			}
			for _, ent := range snap.Entries {
				entryRef, ok := runtime.EntryRefFromEntry(ent)
				if ok && runtime.SameEntryRef(entryRef, ref) {
					return printStateEntry(cmd, ent)
				}
			}
			return fmt.Errorf("no entry at %s", ref.String())
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStatePullCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "pull [revision]",
		Short: "Print a decrypted state snapshot as JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := loadStateStore(info, configPath)
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
			body, err := state.EncodeSnapshot(snap)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(body)
			return err
		},
	}
	addConfigFlag(cmd, &configPath)
	return cmd
}

func loadStateStore(info Info, configPath string) (state.Backend, error) {
	config, err := parseStackFile(configPath)
	if err != nil {
		return nil, err
	}
	enc, err := loadEncrypter(config, configPath)
	if err != nil {
		return nil, err
	}
	return loadStore(info, config, configPath, stackName(configPath), enc)
}

func stateEntryRefs(snap *state.Snapshot) ([]string, error) {
	refs := make([]string, 0, len(snap.Entries))
	for i, ent := range snap.Entries {
		ref, ok := runtime.EntryRefFromEntry(ent)
		if !ok {
			return nil, fmt.Errorf("state entry %d is missing a complete ref", i)
		}
		refs = append(refs, ref.String())
	}
	slices.Sort(refs)
	return refs, nil
}

func printStateEntry(cmd *cobra.Command, ent *state.Entry) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "address: %s\n", ent.Address)
	fmt.Fprintf(out, "selector: %s\n", selectorString(ent.Selector))
	fmt.Fprintf(out, "entry-kind: %s\n", ent.Type)
	fmt.Fprintf(out, "node-kind: %s\n", ent.Kind)
	fmt.Fprintf(out, "schema-version: %d\n", ent.SchemaVersion)
	if ent.TriggerHash != "" {
		fmt.Fprintf(out, "trigger-hash: %s\n", ent.TriggerHash)
	}
	printStateMap(out, "inputs", ent.Inputs, ent.SensitiveInputs)
	printStateMap(out, "outputs", ent.Outputs, ent.SensitiveOutputs)
	printStateList(out, "depends-on", ent.DependsOn)
	printStateList(out, "sensitive-inputs", ent.SensitiveInputs)
	printStateList(out, "sensitive-outputs", ent.SensitiveOutputs)
	return nil
}

func selectorString(sel *state.Selector) string {
	if sel == nil {
		return ""
	}
	return sel.Alias + "." + sel.Export
}

func printStateMap(
	out io.Writer,
	name string,
	values map[string]any,
	sensitive []string,
) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n", name)
	keys := sortedMapKeys(values)
	sensitiveSet := map[string]bool{}
	for _, key := range sensitive {
		sensitiveSet[key] = true
	}
	for _, key := range keys {
		value := sensitivePlaceholder
		if !sensitiveSet[key] {
			value = strings.ReplaceAll(lang.RenderPretty(values[key]), "\n", "\n  ")
		}
		fmt.Fprintf(out, "  %s: %s\n", key, value)
	}
}

func printStateList(out io.Writer, name string, values []string) {
	if len(values) == 0 {
		return
	}
	items := append([]string(nil), values...)
	slices.Sort(items)
	fmt.Fprintf(out, "%s: %s\n", name, strings.Join(items, ", "))
}
