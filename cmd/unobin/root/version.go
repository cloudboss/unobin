package root

import (
	"github.com/spf13/cobra"
)

// Version is the build time version string. Set via -ldflags.
var Version = "dev"

var (
	VersionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the unobin version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(Version)
		},
	}
)
