package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var sopDevCmd = &cobra.Command{
	Use:   "dev",
	Short: "Developer kit for symbolic SoP authors",
}

var sopDevInitCmd = &cobra.Command{
	Use:   "init <directory>",
	Short: "Create a symbolic SoP in an empty or nonexistent directory",
	Long:  "Create a minimal symbolic SoP by default. Use --feature-rich for parameters, branching and review. Both templates include DEVELOPMENT.md for local lookup.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rich, _ := cmd.Flags().GetBool("feature-rich")
		sop, err := cli.InitSOP(args[0], rich)
		if err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), map[string]string{
			"name": sop.Name, "path": sop.Path, "guide": filepath.Join(sop.Path, "DEVELOPMENT.md"),
		})
	},
}

func newSOPDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Read the developer guide and template examples",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), cli.SOPDeveloperDocs())
			return err
		},
	}
}

var sopDevValidateCmd = &cobra.Command{
	Use:   "validate <directory>",
	Short: "Check skill metadata, Python imports and Workflow definitions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		err := cli.ValidateSOP(cmd.Context(), args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &sopExitError{exit.ExitCode()}
		}
		return err
	},
}

func init() {
	sopDevInitCmd.Flags().Bool("minimal", false, "Start with a single state and finish option (default)")
	sopDevInitCmd.Flags().Bool("feature-rich", false, "Start with parameters, branching and review")
	sopDevInitCmd.MarkFlagsMutuallyExclusive("minimal", "feature-rich")
	sopDevCmd.AddCommand(sopDevInitCmd, newSOPDocsCmd(), sopDevValidateCmd)
	sopCmd.AddCommand(sopDevCmd, newSOPDocsCmd())
}
