package root

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the build time version string. Set via -ldflags.
var Version = "dev"

var (
	VersionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the unobin version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(cliVersion())
		},
	}
)

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
