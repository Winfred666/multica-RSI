package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// Decode only public metadata, even if a server accidentally returns a secret.
type pluginExecutionSummary struct {
	Configured bool `json:"configured"`
	Enabled    bool `json:"enabled"`
	RunAs      *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"run_as"`
}

func init() { workspaceCmd.AddCommand(newWorkspacePluginExecutionCmd()) }

func newWorkspacePluginExecutionCmd() *cobra.Command {
	group := &cobra.Command{Use: "plugin-execution", Short: "Configure the PAT shared by hosted plugins in a workspace"}
	for _, action := range []struct{ name, help string }{
		{"status", "Show hosted execution status and the PAT owner"},
		{"enable", "Enable hosted execution with an owner-authorized PAT from stdin"},
		{"disable", "Disable hosted execution and forget the stored PAT without revoking it"},
	} {
		cmd := &cobra.Command{Use: action.name + " [workspace-id|slug|prefix]", Short: action.help, Args: cobra.MaximumNArgs(1), RunE: runWorkspacePluginExecution}
		if action.name == "enable" {
			cmd.Flags().Bool("token-stdin", false, "Read the supplied PAT from stdin; it remains account-wide")
			cmd.Example = "  multica workspace plugin-execution enable acme --token-stdin < /secure/plugin-pat"
		}
		group.AddCommand(cmd)
	}
	return group
}

func runWorkspacePluginExecution(cmd *cobra.Command, args []string) error {
	var token string
	if cmd.Name() == "enable" {
		fromStdin, _ := cmd.Flags().GetBool("token-stdin")
		if !fromStdin {
			return fmt.Errorf("provide --token-stdin with a PAT authorized by its owner")
		}
		raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 257))
		if err != nil {
			return fmt.Errorf("read PAT from stdin: %w", err)
		}
		token = strings.TrimSpace(string(raw))
		if len(raw) > 256 || !strings.HasPrefix(token, "mul_") {
			return fmt.Errorf("stdin must contain a personal access token")
		}
	}
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass a workspace or set MULTICA_WORKSPACE_ID")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	path := "/api/workspaces/" + url.PathEscape(wsID) + "/plugins/execution"
	var status pluginExecutionSummary
	switch cmd.Name() {
	case "status":
		err = client.GetJSON(ctx, path, &status)
	case "enable":
		err = client.PutJSON(ctx, path, map[string]string{"token": token}, &status)
	case "disable":
		err = client.DeleteJSON(ctx, path)
	}
	if err != nil {
		// A proxy echoing a failed request body must not print the supplied PAT.
		message := err.Error()
		if token != "" {
			message = strings.ReplaceAll(message, token, "[REDACTED]")
		}
		return fmt.Errorf("workspace plugin execution: %s", message)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), status)
}
