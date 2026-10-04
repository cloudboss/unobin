package cmdout

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/pkg/filechange"
)

func CommandFormat(cmd *cobra.Command) (Format, error) {
	if cmd.Flags().Lookup("format") == nil {
		return FormatText, nil
	}
	value, err := cmd.Flags().GetString("format")
	if err != nil {
		return "", err
	}
	return ParseFormat(value)
}

func ToolOutput(cmd *cobra.Command, format Format) io.Writer {
	if format.Machine() {
		return io.Discard
	}
	return cmd.ErrOrStderr()
}

func WriteOperationError(cmd *cobra.Command, format Format, files []filechange.Change,
	err error,
) error {
	if !format.Machine() {
		return err
	}
	if files != nil {
		err = WithFiles(err, files)
	}
	return WriteCommandError(cmd, format, nil, err)
}
