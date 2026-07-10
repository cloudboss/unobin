package cmdout

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/spf13/cobra"
)

type failureGolden struct {
	Codes         []Code               `json:"codes"`
	Failures      []failureCaseGolden  `json:"failures"`
	CommandNames  []commandNameGolden  `json:"command-names"`
	CommandErrors []commandErrorGolden `json:"command-errors"`
}

type failureCaseGolden struct {
	Name        string                  `json:"name"`
	Error       string                  `json:"error"`
	Unwrap      string                  `json:"unwrap"`
	AsFailure   bool                    `json:"as-failure"`
	Code        Code                    `json:"code"`
	Message     string                  `json:"message"`
	Diagnostics []diagnostic.Diagnostic `json:"diagnostics"`
	Files       []filechange.Change     `json:"files"`
}

type commandNameGolden struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type commandErrorGolden struct {
	Name       string `json:"name"`
	Format     Format `json:"format"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Error      string `json:"error"`
	Reported   bool   `json:"reported"`
	CauseFound bool   `json:"cause-found"`
}

func TestFailureGolden(t *testing.T) {
	result := failureGolden{Codes: []Code{
		CodeInvalidArgs,
		CodeIO,
		CodeFailed,
		CodeStdoutConflict,
	}}

	cause := errors.New("disk full")
	diagnostics := []diagnostic.Diagnostic{{
		Code: "unobin.io", Severity: diagnostic.SeverityError, Message: "write failed",
	}}
	failure := FailWithDiagnostics(CodeIO, "could not write output", cause, diagnostics)
	diagnostics[0].Message = "mutated"
	result.Failures = append(result.Failures, failureView("with diagnostics", failure))

	withoutCause := Fail(CodeInvalidArgs, "flags conflict", nil)
	result.Failures = append(result.Failures, failureView("without cause", withoutCause))

	withFiles := WithFiles(failure, []filechange.Change{
		{Path: "build/main.go", Action: filechange.ActionCreated},
		{Path: "build/main.go", Action: filechange.ActionUpdated},
		{Path: "build/go.mod", Action: filechange.ActionUnchanged},
	})
	result.Failures = append(result.Failures, failureView("with files", withFiles))

	invalidFiles := WithFiles(failure, []filechange.Change{
		{Path: "build/main.go", Action: filechange.ActionUpdated},
		{Path: "build/main.go", Action: filechange.ActionCreated},
	})
	result.Failures = append(result.Failures, failureView("invalid files", invalidFiles))
	result.Failures = append(result.Failures, failureView(
		"generic with files",
		WithFiles(errors.New("generic"), []filechange.Change{{
			Path: "created.ub", Action: filechange.ActionCreated,
		}}),
	))
	result.Failures = append(result.Failures, failureView("nil files", WithFiles(nil, nil)))
	result.Failures = append(
		result.Failures,
		failureView("wrapped failure", fmt.Errorf("command: %w", withFiles)),
	)

	root, one, two, three := commandTree()
	for _, command := range []*cobra.Command{root, one, two, three} {
		result.CommandNames = append(result.CommandNames, commandNameGolden{
			Path: command.CommandPath(),
			Name: CommandName(command),
		})
	}

	for _, format := range []Format{FormatJSON, FormatUnobin} {
		result.CommandErrors = append(
			result.CommandErrors,
			writeFailureCase(format, withFiles),
		)
	}
	result.CommandErrors = append(
		result.CommandErrors,
		writeFailureCase(FormatJSON, errors.New("generic failure")),
		writeFailureCase(FormatJSON, withoutCause),
		writeEmptyFailureCase(),
		writeFailureWriterCase(),
	)

	requireCmdoutGolden(t, "testdata/failure.json", result)
}

func writeEmptyFailureCase() commandErrorGolden {
	root := &cobra.Command{Use: "unobin"}
	command := &cobra.Command{Use: "compile"}
	root.AddCommand(command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	failure := Fail(CodeInvalidArgs, "flags conflict", nil)
	err := WriteCommandError(command, FormatJSON, nil, failure)
	return commandErrorGolden{
		Name:       "empty required collections",
		Format:     FormatJSON,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Error:      cmdoutErrorString(err),
		Reported:   IsReported(err),
		CauseFound: errors.Is(err, failure),
	}
}

func failureView(name string, err error) failureCaseGolden {
	view := failureCaseGolden{Name: name, Error: cmdoutErrorString(err)}
	if err == nil {
		return view
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		view.Unwrap = unwrapped.Error()
	}
	failure, ok := AsFailure(err)
	view.AsFailure = ok
	if ok {
		view.Code = failure.Code
		view.Message = failure.Message
		view.Diagnostics = failure.Diagnostics
		view.Files = failure.Files
	}
	return view
}

func commandTree() (*cobra.Command, *cobra.Command, *cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "factory"}
	one := &cobra.Command{Use: "state"}
	two := &cobra.Command{Use: "snapshots"}
	three := &cobra.Command{Use: "gc"}
	root.AddCommand(one)
	one.AddCommand(two)
	two.AddCommand(three)
	return root, one, two, three
}

func writeFailureCase(format Format, failure error) commandErrorGolden {
	root := &cobra.Command{Use: "unobin"}
	command := &cobra.Command{Use: "compile"}
	root.AddCommand(command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	parseCause := &parse.Error{
		Kind: parse.ErrSchema,
		Pos:  parse.Position{File: "factory.ub", Line: 3, Column: 5, Offset: 25},
		Msg:  "unknown field size",
	}
	collected := []diagnostic.Diagnostic{{
		Code: "unobin.notice", Severity: diagnostic.SeverityInfo, Message: "collected",
	}}
	if existing, ok := AsFailure(failure); ok {
		clone := *existing
		clone.Cause = errors.Join(existing.Cause, parseCause)
		failure = &clone
	}
	err := WriteCommandError(command, format, collected, failure)
	return commandErrorGolden{
		Name:       "write command error",
		Format:     format,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Error:      cmdoutErrorString(err),
		Reported:   IsReported(err),
		CauseFound: errors.Is(err, failure),
	}
}

func writeFailureWriterCase() commandErrorGolden {
	root := &cobra.Command{Use: "unobin"}
	command := &cobra.Command{Use: "compile"}
	root.AddCommand(command)
	out := &failingWriter{}
	var stderr bytes.Buffer
	command.SetOut(out)
	command.SetErr(&stderr)
	err := WriteCommandError(command, FormatJSON, nil, errors.New("failed"))
	PrintUnreportedError(command, err)
	return commandErrorGolden{
		Name:     "response writer failure",
		Format:   FormatJSON,
		Stdout:   out.String(),
		Stderr:   stderr.String(),
		Error:    cmdoutErrorString(err),
		Reported: IsReported(err),
	}
}
