package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() { workspaceCmd.AddCommand(newWorkspacePluginCmd()) }

func newWorkspacePluginCmd() *cobra.Command {
	group := &cobra.Command{Use: "plugin", Short: "List and configure installed workspace plugins"}
	group.AddCommand(&cobra.Command{Use: "list [workspace-id|slug|prefix]", Args: cobra.MaximumNArgs(1), Short: "List installed plugins and their settings", RunE: runWorkspacePlugin})
	configure := &cobra.Command{Use: "configure <installation-id> [workspace-id|slug|prefix]", Args: cobra.RangeArgs(1, 2), Short: "Update plugin settings from a JSON object", RunE: runWorkspacePlugin}
	configure.Flags().String("config-file", "", "JSON file containing setting names and values")
	_ = configure.MarkFlagRequired("config-file")
	group.AddCommand(configure)
	return group
}

func runWorkspacePlugin(cmd *cobra.Command, args []string) error {
	workspaceArgs := args
	if cmd.Name() == "configure" {
		workspaceArgs = args[1:]
	}
	wsID, err := resolveWorkspaceArg(cmd, workspaceArgs)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	path := "/api/workspaces/" + url.PathEscape(wsID) + "/plugins"
	var response any
	if cmd.Name() == "list" {
		err = client.GetJSON(ctx, path, &response)
	} else {
		file, _ := cmd.Flags().GetString("config-file")
		raw, readErr := os.ReadFile(file)
		if readErr != nil {
			return readErr
		}
		var values map[string]any
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return fmt.Errorf("config-file must contain a JSON object")
		}
		err = client.PutJSON(ctx, path+"/"+url.PathEscape(args[0])+"/config", map[string]any{"values": values}, &response)
	}
	if err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), response)
}
