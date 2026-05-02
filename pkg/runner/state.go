package runner

import (
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
	return cmd
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
