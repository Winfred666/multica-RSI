package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var sopCmd = &cobra.Command{Use: "sop", Short: "Run local symbolic SoPs"}

var sopListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SoP names and source paths",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir, err := os.Getwd()
		if err != nil {
			return err
		}
		sops, err := cli.DiscoverSOPs(workdir, cli.SOPSkillRoots())
		if err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), sops)
	},
}

var sopRunCmd = &cobra.Command{
	Use:   "run <name> [args...]",
	Short: "Execute a SoP",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir, err := os.Getwd()
		if err != nil {
			return err
		}
		sops, err := cli.DiscoverSOPs(workdir, cli.SOPSkillRoots())
		if err != nil {
			return err
		}
		names := make([]string, 0, len(sops))
		for _, sop := range sops {
			names = append(names, sop.Name)
			if sop.Name != args[0] {
				continue
			}
			stateRoot := os.Getenv(cli.TaskConfigRootEnv)
			if stateRoot == "" {
				stateRoot, err = os.UserCacheDir()
				if err != nil {
					return err
				}
				stateRoot = filepath.Join(stateRoot, "multica")
			}
			cfg, err := cli.LoadCLIConfigForProfile(resolveProfile(cmd))
			if err != nil {
				return err
			}
			depth := cfg.SOPMaxActionDepth
			if depth == 0 {
				depth = cli.DefaultSOPMaxActionDepth
			}
			env := append(os.Environ(), "MULTICA_SERVER_URL="+tryResolveServerURL(cmd),
				"MULTICA_WORKSPACE_ID="+resolveWorkspaceID(cmd), "MULTICA_TOKEN="+resolveToken(cmd),
				"MULTICA_SOP_MAX_ACTION_DEPTH="+strconv.Itoa(depth))
			err = cli.RunSOP(cmd.Context(), sop, filepath.Join(stateRoot, "sop", sop.Name), workdir, args[1:], env, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return &sopExitError{exit.ExitCode()}
			}
			return err
		}
		return fmt.Errorf("SOP %q not found. Available SOPs: %s", args[0], strings.Join(names, ", "))
	},
}

type sopExitError struct{ code int }

func (e *sopExitError) Error() string { return fmt.Sprintf("SOP exited with code %d", e.code) }

func init() {
	sopRunCmd.Flags().SetInterspersed(false)
	sopCmd.AddCommand(sopListCmd, sopRunCmd)
}
