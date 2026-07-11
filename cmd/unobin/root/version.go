package root

import (
	"fmt"
	"runtime/debug"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/spf13/cobra"
)

// Version is the build time version string. Set via -ldflags.
var Version = "dev"

var (
	VersionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the unobin version",
		Args:  cobra.NoArgs,
		RunE:  runVersion,
	}
)

type versionResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Name          string                  `json:"name"           ub:"name"`
	Version       string                  `json:"version"        ub:"version"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func init() {
	VersionCmd.Flags().String("format", "text", cmdout.FormatHelp())
}

func runVersion(cmd *cobra.Command, _ []string) error {
	formatValue, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	format, err := cmdout.ParseFormat(formatValue)
	if err != nil {
		return err
	}
	version := cliVersion()
	if format == cmdout.FormatText {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
		return err
	}
	return cmdout.WriteDocument(cmd.OutOrStdout(), format, versionResult{
		Kind:          "version",
		FormatVersion: 1,
		Name:          "unobin",
		Version:       version,
		Diagnostics:   diagnostic.Normalize(nil),
	})
}

// readBuildInfo is swapped by tests to exercise cliVersion without a
// real build.
var readBuildInfo = debug.ReadBuildInfo

// cliVersion returns the version this binary identifies as: the
// link-time stamp when a release set one, else the module version Go
// recorded when the binary was installed from a tagged module, else
// "dev". The same version pins the unobin requirement in every
// generated go.mod, so a factory always runs the runtime its compiler
// checked it with.
func cliVersion() string {
	if Version != "dev" {
		return Version
	}
	info, ok := readBuildInfo()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}
