package root

import (
	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/cmd/unobin/root/deps"
)

var DepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Manage a factory's dependencies",
	Long: `Manage dependency floors in project.ub and selected versions in project-lock.ub.

A factory or UB library writes imports in .ub source. The project records
its direct dependency floors, and project-lock records the versions and source
hashes the compiler should use.`,
}

func init() {
	DepsCmd.AddCommand(deps.SyncCmd, deps.ListCmd, deps.VerifyCmd, deps.CleanCmd, deps.GetCmd)
}
