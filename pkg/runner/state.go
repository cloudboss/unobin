package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			return doStateGCWithFormat(
				cmd, info, configPath, keep, format, collector.Diagnostics(),
			)
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	cmd.Flags().IntVar(&keep, "keep", 10,
		"Number of recent snapshot revisions to keep. The current revision"+
			" is always kept in addition to these.")
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateGCWithFormat(
	cmd *cobra.Command,
	info Info,
	configPath string,
	keep int,
	format cmdout.Format,
	diagnostics []diagnostic.Diagnostic,
) error {
	result, err := gcState(info, configPath, keep)
	if !format.Machine() {
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d snapshot(s), kept %d.\n",
			result.Deleted, result.Kept)
		return nil
	}
	if result == nil {
		return stateCommandFailure(cmd, format, diagnostics, err)
	}
	resultDiagnostics := diagnostics
	if err != nil {
		resultDiagnostics = diagnostic.Merge(diagnostics, stateErrorDiagnostics(err))
	}
	document := buildStateGCResult(
		info, result.Stack, err == nil, result.Deleted, result.Kept,
		result.Current, result.FailedRevision, resultDiagnostics,
	)
	if writeErr := cmdout.WriteDocument(cmd.OutOrStdout(), format, document); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return cmdout.Reported(err)
	}
	return nil
}

type stateGCMutation struct {
	Stack          string
	Deleted        int
	Kept           int
	Current        *string
	FailedRevision *string
}

func gcState(
	info Info,
	configPath string,
	keep int,
) (result *stateGCMutation, err error) {
	if keep < 0 {
		return nil, fmt.Errorf("--keep must not be negative")
	}
	metadata, err := loadStateMetadata(info, configPath)
	if err != nil {
		return nil, err
	}
	return gcStateMetadata(metadata, keep)
}

func gcStateMetadata(
	metadata stateMetadata,
	keep int,
) (result *stateGCMutation, err error) {
	release, err := runtime.AcquireStateLock(context.Background(), metadata.Store)
	if err != nil {
		return nil, err
	}
	defer func() {
		operationErr := err
		err = release(err)
		if operationErr == nil && err != nil && result != nil && result.Deleted == 0 {
			result = nil
		}
	}()

	revs, err := metadata.Store.List()
	if err != nil {
		return nil, err
	}
	if revs == nil {
		revs = []string{}
	}
	current, err := currentStateRevision(metadata.Store)
	if err != nil {
		return nil, err
	}

	keepSet := map[string]bool{}
	if current != nil {
		keepSet[*current] = true
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
		if err := metadata.Store.Delete(r); err != nil {
			if deleted > 0 {
				failedRevision := r
				result = &stateGCMutation{
					Stack: metadata.Stack, Deleted: deleted, Kept: len(revs) - deleted,
					Current: current, FailedRevision: &failedRevision,
				}
			}
			return result, err
		}
		deleted++
	}
	return &stateGCMutation{
		Stack: metadata.Stack, Deleted: deleted, Kept: len(revs) - deleted,
		Current: current,
	}, nil
}

func newStateMoveCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "move <from-state-ref> <to-state-ref>",
		Short: "Move a state entry to a new address",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			return doStateMoveWithFormat(
				cmd, info, configPath, args[0], args[1], format, collector.Diagnostics(),
			)
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateMoveWithFormat(
	cmd *cobra.Command,
	info Info,
	configPath string,
	fromText string,
	toText string,
	format cmdout.Format,
	diagnostics []diagnostic.Diagnostic,
) error {
	result, err := moveState(info, configPath, fromText, toText)
	if !format.Machine() {
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if result.Moved == 1 {
			fmt.Fprintf(out, "Moved %s to %s.\n", result.From, result.To)
		} else {
			fmt.Fprintf(out, "Moved %s to %s (%d entries).\n",
				result.From, result.To, result.Moved)
		}
		return nil
	}
	if result == nil {
		return stateCommandFailure(cmd, format, diagnostics, err)
	}
	resultDiagnostics := diagnostics
	if err != nil {
		resultDiagnostics = diagnostic.Merge(diagnostics, stateErrorDiagnostics(err))
	}
	document, buildErr := buildStateMoveResult(
		info, result.Stack, err == nil, result.From, result.To, result.Moved,
		result.StateRev, resultDiagnostics,
	)
	if buildErr != nil {
		return stateCommandFailure(cmd, format, diagnostics, buildErr)
	}
	if writeErr := cmdout.WriteDocument(cmd.OutOrStdout(), format, document); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return cmdout.Reported(err)
	}
	return nil
}

type stateMoveMutation struct {
	Stack    string
	From     string
	To       string
	Moved    int
	StateRev string
}

func moveState(
	info Info,
	configPath string,
	fromText string,
	toText string,
) (result *stateMoveMutation, err error) {
	from, err := runtime.ParseEntryRef(fromText)
	if err != nil {
		return nil, diagnostic.Context("state move", err)
	}
	to, err := runtime.ParseEntryRef(toText)
	if err != nil {
		return nil, diagnostic.Context("state move", err)
	}
	parsed, err := parseFactory(info)
	if err != nil {
		return nil, err
	}
	metadata, err := loadStateMetadata(info, configPath)
	if err != nil {
		return nil, err
	}
	return moveStateMetadata(metadata, parsed.dag, info.Libraries, from, to)
}

func moveStateMetadata(
	metadata stateMetadata,
	dag *runtime.DAG,
	libraries map[string]*runtime.Library,
	from runtime.EntryRef,
	to runtime.EntryRef,
) (result *stateMoveMutation, err error) {
	release, err := runtime.AcquireStateLock(context.Background(), metadata.Store)
	if err != nil {
		return nil, err
	}
	startingRevision, err := currentStateRevision(metadata.Store)
	if err != nil {
		return nil, release(err)
	}
	movedCount := 0
	defer func() {
		err = release(err)
		if err == nil || result != nil {
			return
		}
		current, currentErr := currentStateRevision(metadata.Store)
		if currentErr != nil {
			err = errors.Join(err, currentErr)
			return
		}
		if !sameOptionalString(startingRevision, current) && current != nil {
			result = &stateMoveMutation{
				Stack: metadata.Stack, From: from.String(), To: to.String(),
				Moved: movedCount, StateRev: *current,
			}
		}
	}()

	snap, err := metadata.Store.Current()
	if err != nil {
		return nil, err
	}
	next, moved, err := runtime.ApplyEntryMoves(
		snap,
		dag,
		libraries,
		[]runtime.EntryMoveSpec{{From: from, To: to}},
		runtime.EntryMoveStrict,
	)
	if err != nil {
		return nil, err
	}
	movedCount = len(moved)

	rev, err := metadata.Store.Write(next)
	if err != nil {
		return nil, err
	}
	if err := metadata.Store.SetCurrent(rev); err != nil {
		return nil, err
	}
	return &stateMoveMutation{
		Stack: metadata.Stack, From: from.String(), To: to.String(),
		Moved: len(moved), StateRev: rev,
	}, nil
}

func newStateRemoveCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove <state-ref>",
		Short: "Remove a state entry without touching the underlying resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			return doStateRemoveWithFormat(
				cmd, info, configPath, args[0], format, collector.Diagnostics(),
			)
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	addConfigFlag(cmd, &configPath)
	return cmd
}

func doStateRemoveWithFormat(
	cmd *cobra.Command,
	info Info,
	configPath string,
	refText string,
	format cmdout.Format,
	diagnostics []diagnostic.Diagnostic,
) error {
	result, err := removeState(info, configPath, refText)
	if !format.Machine() {
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", result.Address)
		return nil
	}
	if result == nil {
		return stateCommandFailure(cmd, format, diagnostics, err)
	}
	resultDiagnostics := diagnostics
	if err != nil {
		resultDiagnostics = diagnostic.Merge(diagnostics, stateErrorDiagnostics(err))
	}
	document, buildErr := buildStateRemoveResult(
		info, result.Stack, err == nil, result.Address, result.StateRev, resultDiagnostics,
	)
	if buildErr != nil {
		return stateCommandFailure(cmd, format, diagnostics, buildErr)
	}
	if writeErr := cmdout.WriteDocument(cmd.OutOrStdout(), format, document); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return cmdout.Reported(err)
	}
	return nil
}

type stateRemoveMutation struct {
	Stack    string
	Address  string
	StateRev string
}

func removeState(
	info Info,
	configPath string,
	refText string,
) (result *stateRemoveMutation, err error) {
	ref, err := runtime.ParseEntryRef(refText)
	if err != nil {
		return nil, diagnostic.Context("state remove", err)
	}
	metadata, err := loadStateMetadata(info, configPath)
	if err != nil {
		return nil, err
	}
	return removeStateMetadata(metadata, ref)
}

func removeStateMetadata(
	metadata stateMetadata,
	ref runtime.EntryRef,
) (result *stateRemoveMutation, err error) {
	release, err := runtime.AcquireStateLock(context.Background(), metadata.Store)
	if err != nil {
		return nil, err
	}
	startingRevision, err := currentStateRevision(metadata.Store)
	if err != nil {
		return nil, release(err)
	}
	defer func() {
		err = release(err)
		if err == nil || result != nil {
			return
		}
		current, currentErr := currentStateRevision(metadata.Store)
		if currentErr != nil {
			err = errors.Join(err, currentErr)
			return
		}
		if !sameOptionalString(startingRevision, current) && current != nil {
			result = &stateRemoveMutation{
				Stack: metadata.Stack, Address: ref.String(), StateRev: *current,
			}
		}
	}()

	snap, err := metadata.Store.Current()
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("no entry at %s", ref.String())
	}
	snap.Entries = append(snap.Entries[:idx], snap.Entries[idx+1:]...)

	rev, err := metadata.Store.Write(snap)
	if err != nil {
		return nil, err
	}
	if err := metadata.Store.SetCurrent(rev); err != nil {
		return nil, err
	}
	return &stateRemoveMutation{
		Stack: metadata.Stack, Address: ref.String(), StateRev: rev,
	}, nil
}

func newStateForceUnlockCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "force-unlock",
		Short: "Remove the stack's lock without checking who holds it",
		Args:  cobra.NoArgs,
		Long: "Use this only when a previous run died without releasing the lock. " +
			"Make sure no apply or refresh is running against this stack first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			metadata, err := loadStateMetadata(info, configPath)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			if err := metadata.Store.ForceUnlock(); err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			if format.Machine() {
				return cmdout.WriteDocument(
					cmd.OutOrStdout(), format,
					buildStateForceUnlockResult(
						info, metadata.Stack, collector.Diagnostics(),
					),
				)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Lock cleared.")
			return nil
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateListCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List current state entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			metadata, err := loadStateMetadata(info, configPath)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			revision, err := currentStateRevision(metadata.Store)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			var snapshot *state.Snapshot
			if revision != nil {
				snapshot, err = metadata.Store.Current()
				if err != nil {
					return commandResultFailure(cmd, format, collector.Diagnostics(), err)
				}
			}
			if format.Machine() {
				result, err := buildStateListResult(
					info, metadata.Stack, revision, snapshot, collector.Diagnostics(),
				)
				if err != nil {
					return commandResultFailure(cmd, format, collector.Diagnostics(), err)
				}
				return cmdout.WriteDocument(cmd.OutOrStdout(), format, result)
			}
			if snapshot == nil {
				return nil
			}
			entries, err := sortedStateEntries(snapshot)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, ent := range entries {
				fmt.Fprintln(out, stateEntryListLine(ent))
			}
			return nil
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateSnapshotsListCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshot revisions, marking the current one with *",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			metadata, err := loadStateMetadata(info, configPath)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			revs, err := metadata.Store.List()
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			if revs == nil {
				revs = []string{}
			}
			current, err := currentStateRevision(metadata.Store)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			if format.Machine() {
				return cmdout.WriteDocument(
					cmd.OutOrStdout(), format,
					buildStateSnapshotsResult(
						info, metadata.Stack, current, revs, collector.Diagnostics(),
					),
				)
			}
			out := cmd.OutOrStdout()
			for _, r := range revs {
				marker := "  "
				if current != nil && r == *current {
					marker = "* "
				}
				fmt.Fprintf(out, "%s%s\n", marker, r)
			}
			return nil
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
	addConfigFlag(cmd, &configPath)
	return cmd
}

func newStateShowCmd(info Info) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "show <state-ref>",
		Short: "Show one current state entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, collector, err := beginCommandResult(cmd, info)
			if err != nil {
				return err
			}
			ref, err := runtime.ParseEntryRef(args[0])
			if err != nil {
				return commandResultFailure(
					cmd, format, collector.Diagnostics(), diagnostic.Context("state show", err),
				)
			}
			metadata, err := loadStateMetadata(info, configPath)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			revision, err := currentStateRevision(metadata.Store)
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			if revision == nil {
				return commandResultFailure(
					cmd, format, collector.Diagnostics(), state.ErrNoCurrent,
				)
			}
			snap, err := metadata.Store.Current()
			if err != nil {
				return commandResultFailure(cmd, format, collector.Diagnostics(), err)
			}
			for _, ent := range snap.Entries {
				entryRef, ok := runtime.EntryRefFromEntry(ent)
				if ok && runtime.SameEntryRef(entryRef, ref) {
					if format.Machine() {
						result, err := buildStateEntryResult(
							info, metadata.Stack, *revision, ent, collector.Diagnostics(),
						)
						if err != nil {
							return commandResultFailure(
								cmd, format, collector.Diagnostics(), err,
							)
						}
						return cmdout.WriteDocument(cmd.OutOrStdout(), format, result)
					}
					return printStateEntry(cmd, ent)
				}
			}
			return commandResultFailure(
				cmd, format, collector.Diagnostics(), fmt.Errorf("no entry at %s", ref.String()),
			)
		},
	}
	ownStartupCheck(cmd)
	addStandardFormatFlag(cmd)
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
	metadata, err := loadStateMetadata(info, configPath)
	if err != nil {
		return nil, err
	}
	return metadata.Store, nil
}

type stateMetadata struct {
	Store state.Backend
	Stack string
}

func loadStateMetadata(info Info, configPath string) (stateMetadata, error) {
	config, err := parseStackFile(configPath)
	if err != nil {
		return stateMetadata{}, err
	}
	enc, err := loadEncrypter(config, configPath)
	if err != nil {
		return stateMetadata{}, err
	}
	stack := stackName(configPath)
	store, err := loadStore(info, config, configPath, stack, enc)
	if err != nil {
		return stateMetadata{}, err
	}
	return stateMetadata{Store: store, Stack: stack}, nil
}

func currentStateRevision(store state.Backend) (*string, error) {
	revision, err := store.CurrentRev()
	if errors.Is(err, state.ErrNoCurrent) {
		return nil, nil
	}
	if err != nil {
		return nil, diagnostic.Context("current revision", err)
	}
	return &revision, nil
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func stateErrorDiagnostics(err error) []diagnostic.Diagnostic {
	diagnostics := diagnostic.FromError(err, diagnostic.ConvertOptions{})
	var unlockError *runtime.StateUnlockError
	if !errors.As(err, &unlockError) {
		return diagnostics
	}
	for i := range diagnostics {
		if diagnostics[i].Message == unlockError.Error() {
			diagnostics[i].Code = "unobin.state.unlock"
		}
	}
	return diagnostic.Normalize(diagnostics)
}

func stateCommandFailure(
	cmd *cobra.Command,
	format cmdout.Format,
	collected []diagnostic.Diagnostic,
	err error,
) error {
	var unlockError *runtime.StateUnlockError
	if !format.Machine() || !errors.As(err, &unlockError) {
		return commandResultFailure(cmd, format, collected, err)
	}
	failure := cmdout.FailWithDiagnostics(
		cmdout.CodeFailed, "", nil, stateErrorDiagnostics(err),
	)
	return cmdout.WriteCommandError(cmd, format, collected, failure)
}

type listedStateEntry struct {
	ref string
	ent *state.Entry
}

func sortedStateEntries(snap *state.Snapshot) ([]*state.Entry, error) {
	entries := make([]listedStateEntry, 0, len(snap.Entries))
	for i, ent := range snap.Entries {
		ref, ok := runtime.EntryRefFromEntry(ent)
		if !ok {
			return nil, fmt.Errorf("state entry %d is missing a valid state ref", i)
		}
		entries = append(entries, listedStateEntry{ref: ref.String(), ent: ent})
	}
	slices.SortFunc(entries, func(a, b listedStateEntry) int {
		return strings.Compare(a.ref, b.ref)
	})
	out := make([]*state.Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.ent)
	}
	return out, nil
}

func stateEntryListLine(ent *state.Entry) string {
	if ent.Binding == nil || ent.Binding.Alias == "" || ent.Binding.Export == "" {
		return ent.Address
	}
	return fmt.Sprintf("%s (%s.%s)", ent.Address, ent.Binding.Alias, ent.Binding.Export)
}

func printStateEntry(cmd *cobra.Command, ent *state.Entry) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "address: %s\n", ent.Address)
	if ent.Binding != nil {
		fmt.Fprintf(out, "import-alias: %s\n", ent.Binding.Alias)
		fmt.Fprintf(out, "library-path: %s\n", ent.Binding.LibraryPath)
		fmt.Fprintf(out, "kind: %s\n", ent.Binding.Export)
	}
	fmt.Fprintf(out, "category: %s\n", ent.Category)
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
