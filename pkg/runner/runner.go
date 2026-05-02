// Package runner is the CLI scaffolding every compiled stack binary
// links into. The generated `main.go` stays tiny: it embeds the
// stack-specific constants and calls Run with them.
package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/state"
	"github.com/spf13/cobra"
)

// Info bundles everything a generated stack binary passes into Run.
type Info struct {
	StackName    string
	StackVersion string
	StackCommit  string
	StackSource  string
	Modules      map[string]*runtime.Module
}

// Run builds the cobra command tree and executes it. Exits the process
// non-zero on error.
func Run(info Info) {
	root := newRootCmd(info)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(info Info) *cobra.Command {
	root := &cobra.Command{
		Use:           info.StackName,
		Short:         "Compiled unobin stack " + info.StackName,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd(info))
	root.AddCommand(newApplyCmd(info))
	root.AddCommand(newOutputCmd(info))
	return root
}

func newVersionCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print stack identity",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s (commit %s)\n",
				info.StackName, info.StackVersion, info.StackCommit)
		},
	}
}

func newApplyCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Run the stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doApply(cmd, info)
		},
	}
}

func newOutputCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "output [name]",
		Short: "Print stack outputs from the current state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doOutput(cmd, info, args)
		},
	}
}

func parsedFile(info Info) (*lang.File, error) {
	f, err := lang.ParseSource("stack.ub", []byte(info.StackSource))
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return f, nil
}

func loadStore(info Info) (*state.LocalStore, error) {
	return state.NewLocalStore(".unobin/state", info.StackName, "default", state.NoopEncrypter{})
}

func doApply(cmd *cobra.Command, info Info) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	store, err := loadStore(info)
	if err != nil {
		return err
	}
	exec := &runtime.Executor{
		DAG:     runtime.BuildDAG(f),
		Modules: info.Modules,
		Store:   store,
		Stack: state.StackInfo{
			Name:    info.StackName,
			Version: info.StackVersion,
			Commit:  info.StackCommit,
		},
	}
	res, err := exec.Run(context.Background())
	if err != nil {
		return err
	}
	for k, v := range res.Outputs {
		fmt.Fprintf(cmd.OutOrStdout(), "%s = %v\n", k, v)
	}
	return nil
}

func doOutput(cmd *cobra.Command, info Info, args []string) error {
	store, err := loadStore(info)
	if err != nil {
		return err
	}
	snap, err := store.Current()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for k, v := range snap.Outputs {
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %v\n", k, v)
		}
		return nil
	}
	name := args[0]
	val, ok := snap.Outputs[name]
	if !ok {
		return fmt.Errorf("no output %q", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%v\n", val)
	return nil
}
