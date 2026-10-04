package deps

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/project"
)

var (
	verifyCfg = &verifyConfig{}
	VerifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check the cached dependencies against project-lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd, verifyCfg)
		},
	}
)

type verifyConfig struct {
	stackPath     string
	replaceUnobin string
}

type verifyResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OK            bool                    `json:"ok"             ub:"ok"`
	Checked       int                     `json:"checked"        ub:"checked"`
	Mismatches    []deps.VerifyMismatch   `json:"mismatches"     ub:"mismatches"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

var errDependencyVerification = errors.New("dependency verification failed")

func init() {
	VerifyCmd.Flags().String("format", "text", cmdout.FormatHelp())
	VerifyCmd.Flags().StringVarP(
		&verifyCfg.stackPath, "path", "p", ".", cmdconfig.DependencyPathHelp)
	VerifyCmd.Flags().StringVar(
		&verifyCfg.replaceUnobin, "replace-unobin", "", cmdconfig.DependencyReplacementHelp)
}

func runVerify(cmd *cobra.Command, cfg *verifyConfig) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	verified, err := project.VerifyDependencies(
		cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin))
	if err != nil {
		return cmdout.WriteOperationError(cmd, format, nil, err)
	}
	result := verifyResult{
		Kind:          "dependency-verify-result",
		FormatVersion: 1,
		OK:            len(verified.Mismatches) == 0,
		Checked:       verified.Checked,
		Mismatches:    verified.Mismatches,
		Diagnostics:   diagnostic.Merge(),
	}
	if format.Machine() {
		if err := cmdout.WriteDocument(cmd.OutOrStdout(), format, result); err != nil {
			return err
		}
		if !result.OK {
			return cmdout.Reported(errDependencyVerification)
		}
		return nil
	}
	if !result.OK {
		messages := make([]string, 0, len(result.Mismatches))
		for _, mismatch := range result.Mismatches {
			messages = append(messages, fmt.Sprintf(
				"%s: hash mismatch (selected %s, got %s)",
				mismatch.ID, mismatch.ExpectedHash, mismatch.ActualHash,
			))
		}
		return fmt.Errorf("verification failed:\n  %s", strings.Join(messages, "\n  "))
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "all dependencies verified")
	return nil
}
