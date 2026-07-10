package cmdout

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/spf13/cobra"
)

type Code string

const (
	CodeInvalidArgs    Code = "unobin.command.invalid-args"
	CodeIO             Code = "unobin.command.io"
	CodeFailed         Code = "unobin.command.failed"
	CodeStdoutConflict Code = "unobin.command.stdout-conflict"
)

type Failure struct {
	Code        Code
	Message     string
	Cause       error
	Diagnostics []diagnostic.Diagnostic
	Files       []filechange.Change
}

func (e *Failure) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Message
}

func (e *Failure) Unwrap() error {
	return e.Cause
}

func Fail(code Code, message string, cause error) error {
	return &Failure{Code: code, Message: message, Cause: cause}
}

func FailWithDiagnostics(
	code Code,
	message string,
	cause error,
	diagnostics []diagnostic.Diagnostic,
) error {
	return &Failure{
		Code:        code,
		Message:     message,
		Cause:       cause,
		Diagnostics: append([]diagnostic.Diagnostic{}, diagnostics...),
	}
}

func WithFiles(err error, files []filechange.Change) error {
	if err == nil {
		return nil
	}
	failure, ok := AsFailure(err)
	all := append([]filechange.Change{}, files...)
	if ok {
		all = append(append([]filechange.Change{}, failure.Files...), files...)
	}
	composed, composeErr := filechange.Compose(all)
	if composeErr != nil {
		return fmt.Errorf("command failure files: %w", composeErr)
	}
	if !ok {
		return &Failure{Code: CodeFailed, Cause: err, Files: composed}
	}
	clone := *failure
	clone.Diagnostics = append([]diagnostic.Diagnostic{}, failure.Diagnostics...)
	clone.Files = composed
	return &clone
}

func AsFailure(err error) (*Failure, bool) {
	var failure *Failure
	ok := errors.As(err, &failure)
	return failure, ok
}

type CommandError struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Command       string                  `json:"command"        ub:"command"`
	Code          Code                    `json:"code"           ub:"code"`
	Message       string                  `json:"message"        ub:"message"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
}

func CommandName(cmd *cobra.Command) string {
	parts := strings.Fields(cmd.CommandPath())
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[1:], " ")
}

func WriteCommandError(
	cmd *cobra.Command,
	format Format,
	collected []diagnostic.Diagnostic,
	failure error,
) error {
	command := CommandName(cmd)
	summary := "command failed"
	if command != "" {
		summary = command + " failed"
	}
	if failure == nil {
		failure = errors.New(summary)
	}
	commandFailure, ok := AsFailure(failure)
	if !ok {
		commandFailure = &Failure{
			Code:    CodeFailed,
			Message: summary,
			Cause:   failure,
		}
	}
	code := commandFailure.Code
	if code == "" {
		code = CodeFailed
	}
	message := commandFailure.Message
	if message == "" {
		message = summary
	}
	defaultDiagnosticCode := "unobin.error"
	if code == CodeIO {
		defaultDiagnosticCode = "unobin.io"
	}
	diagnostics := diagnostic.Merge(
		collected,
		commandFailure.Diagnostics,
		diagnostic.FromError(commandFailure.Cause, diagnostic.ConvertOptions{
			DefaultCode: defaultDiagnosticCode,
		}),
	)
	files, err := filechange.Compose(commandFailure.Files)
	if err != nil {
		return fmt.Errorf("command error files: %w", err)
	}
	response := CommandError{
		Kind:          "command-error",
		FormatVersion: 1,
		Command:       command,
		Code:          code,
		Message:       message,
		Diagnostics:   diagnostics,
		Files:         files,
	}
	if err := WriteDocument(cmd.OutOrStdout(), format, response); err != nil {
		return err
	}
	return Reported(failure)
}
