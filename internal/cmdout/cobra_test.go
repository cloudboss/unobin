package cmdout

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

type cobraGolden struct {
	Reported []reportedGolden `json:"reported"`
	Runs     []cobraRunGolden `json:"runs"`
}

type reportedGolden struct {
	Name       string `json:"name"`
	Error      string `json:"error"`
	Reported   bool   `json:"reported"`
	CauseFound bool   `json:"cause-found"`
	Stderr     string `json:"stderr"`
	Panicked   bool   `json:"panicked"`
	Panic      string `json:"panic"`
}

type cobraRunGolden struct {
	Name     string   `json:"name"`
	Args     []string `json:"args"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	Error    string   `json:"error"`
	Reported bool     `json:"reported"`
}

func TestCobraBoundaryGolden(t *testing.T) {
	cause := errors.New("cause")
	reported := Reported(cause)
	result := cobraGolden{}
	result.Reported = append(result.Reported,
		reportedView("reported", reported, cause),
		reportedView("wrapped reported", fmt.Errorf("outer: %w", reported), cause),
		reportedView("ordinary", cause, cause),
		reportedView("nil", nil, cause),
	)
	panicked, panicValue := capturePanic(func() { _ = Reported(nil) })
	result.Reported = append(result.Reported, reportedGolden{
		Name: "nil reported requirement", Panicked: panicked, Panic: panicValue,
	})

	cases := []struct {
		name string
		args []string
	}{
		{name: "help remains text", args: []string{"command", "--help", "--format", "json"}},
		{
			name: "unknown flag remains text",
			args: []string{"command", "--format", "json", "--unknown"},
		},
		{
			name: "wrong positional count remains text",
			args: []string{"command", "--format", "json", "extra"},
		},
		{name: "unknown format is text", args: []string{"command", "--format", "yaml"}},
		{
			name: "JSON application failure",
			args: []string{"command", "--format", "json", "--fail"},
		},
		{
			name: "Unobin application failure",
			args: []string{"command", "--format", "unobin", "--fail"},
		},
		{name: "text application failure", args: []string{"command", "--fail"}},
		{name: "text success", args: []string{"command"}},
	}
	for _, tc := range cases {
		result.Runs = append(result.Runs, runCobraCase(tc.name, tc.args))
	}

	requireCmdoutGolden(t, "testdata/cobra.json", result)
}

func reportedView(name string, err, cause error) reportedGolden {
	command := &cobra.Command{Use: "tool"}
	var stderr bytes.Buffer
	command.SetErr(&stderr)
	PrintUnreportedError(command, err)
	return reportedGolden{
		Name:       name,
		Error:      cmdoutErrorString(err),
		Reported:   IsReported(err),
		CauseFound: errors.Is(err, cause),
		Stderr:     stderr.String(),
	}
}

func capturePanic(run func()) (panicked bool, value string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = true
			value = fmt.Sprint(recovered)
		}
	}()
	run()
	return false, ""
}

func runCobraCase(name string, args []string) cobraRunGolden {
	root := newBoundaryRoot()
	root.SetArgs(args)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.Execute()
	PrintUnreportedError(root, err)
	return cobraRunGolden{
		Name:     name,
		Args:     args,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Error:    cmdoutErrorString(err),
		Reported: IsReported(err),
	}
}

func newBoundaryRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	var formatValue string
	var fail bool
	command := &cobra.Command{
		Use:   "command",
		Short: "Exercise the command boundary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := ParseFormat(formatValue)
			if err != nil {
				return err
			}
			if !fail {
				fmt.Fprintln(cmd.OutOrStdout(), "ok")
				return nil
			}
			failure := Fail(CodeFailed, "command failed", errors.New("application failed"))
			if format.Machine() {
				return WriteCommandError(cmd, format, nil, failure)
			}
			return failure
		},
	}
	command.Flags().StringVar(&formatValue, "format", "text", FormatHelp())
	command.Flags().BoolVar(&fail, "fail", false, "Fail after RunE begins.")
	root.AddCommand(command)
	return root
}
