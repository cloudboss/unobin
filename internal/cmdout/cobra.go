package cmdout

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

type ReportedError struct {
	Err error
}

func (e *ReportedError) Error() string {
	return e.Err.Error()
}

func (e *ReportedError) Unwrap() error {
	return e.Err
}

func Reported(err error) error {
	if err == nil {
		panic("cmdout: reported error must not be nil")
	}
	return &ReportedError{Err: err}
}

func IsReported(err error) bool {
	var reported *ReportedError
	return errors.As(err, &reported)
}

func PrintUnreportedError(cmd *cobra.Command, err error) {
	if err == nil || IsReported(err) {
		return
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
}
