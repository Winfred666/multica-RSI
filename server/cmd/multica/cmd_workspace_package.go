package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func init() { workspaceCmd.AddCommand(newWorkspacePackageCommands()...) }

func newWorkspacePackageCommands() []*cobra.Command {
	inventory := &cobra.Command{
		Use: "package [workspace-id|slug|prefix]", Short: "Inspect workspace package counts and resource summaries",
		Long: "Returns counts by default. Use --category with --limit and --offset to page through projects, issues, agents, skills, MCPs or plugins. Use --default to download the embedded startup package.",
		Args: cobra.MaximumNArgs(1), RunE: runWorkspacePackage,
	}
	inventory.Flags().Bool("default", false, "Return the embedded harness-only startup package")
	export := &cobra.Command{
		Use: "export [workspace-id|slug|prefix]", Short: "Export selected workspace resources as a JSON package",
		Long: "Selects nothing by default. --selection-file uses the backend selection object: projects, issues, agents, skills, mcps and plugins ID arrays, and the system_prompt boolean. Selection does not expand relationships; references to unselected resources are omitted. Packages can contain private instructions and MCP configuration.",
		Args: cobra.MaximumNArgs(1), RunE: runWorkspacePackage,
	}
	export.Flags().String("file", "", "Write the package to a file instead of stdout")
	importCmd := &cobra.Command{
		Use: "import [workspace-id|slug|prefix]", Short: "Import a workspace JSON package",
		Long: "Imports everything by default. --selection-file uses the same selection object as export; {} imports nothing. Imported agents remain offline and require runtime setup. Matching skill/MCP names and plugin keys in the destination workspace are replaced.",
		Args: cobra.MaximumNArgs(1), RunE: runWorkspacePackage,
	}
	importCmd.Flags().String("file", "", "Package JSON file to import")
	_ = importCmd.MarkFlagRequired("file")
	importCmd.Flags().Bool("inspect", false, "Validate and inspect the package without importing")
	addWorkspaceImportFlags(importCmd)
	importCmd.MarkFlagsMutuallyExclusive("inspect", "activate-plugins")
	for _, cmd := range []*cobra.Command{inventory, importCmd} {
		cmd.Flags().String("category", "", "Summary category: projects, issues, agents, skills, mcps or plugins (import requires --inspect)")
		cmd.Flags().Int("limit", 50, "Summary page size (1–100)")
		cmd.Flags().Int("offset", 0, "Summary page offset")
	}
	for _, flag := range []string{"category", "limit", "offset"} {
		inventory.MarkFlagsMutuallyExclusive("default", flag)
	}
	export.Flags().String("selection-file", "", "JSON file with the backend resource selection")
	for _, cmd := range []*cobra.Command{inventory, export, importCmd} {
		cmd.Flags().String("output", "json", "Output format: json")
	}
	return []*cobra.Command{inventory, export, importCmd}
}

func addWorkspaceImportFlags(cmd *cobra.Command) {
	cmd.Flags().String("selection-file", "", "JSON file with the backend resource selection")
	cmd.Flags().Bool("activate-plugins", false, "Activate hosted plugins using the member PAT")
}

func readWorkspacePackageJSON(path string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", path)
	}
	return json.RawMessage(raw), nil
}

func runWorkspacePackage(cmd *cobra.Command, args []string) error {
	if output, _ := cmd.Flags().GetString("output"); output != "json" {
		return fmt.Errorf("--output must be json")
	}
	var body map[string]any
	var selection json.RawMessage
	var err error
	suffix := "/" + cmd.Name()
	inspect, _ := cmd.Flags().GetBool("inspect")
	if cmd.Name() == "import" {
		file, _ := cmd.Flags().GetString("file")
		body, err = buildWorkspaceImportBody(cmd, file)
		if inspect {
			suffix += "/inspect"
		}
	} else if cmd.Name() == "export" {
		selection, err = readWorkspaceSelection(cmd)
		if selection == nil {
			selection = json.RawMessage(`{}`)
		}
	}
	if err != nil {
		return err
	}
	if cmd.Name() == "package" || inspect {
		category, _ := cmd.Flags().GetString("category")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		if category == "" && (cmd.Flags().Changed("limit") || cmd.Flags().Changed("offset")) {
			return fmt.Errorf("--limit and --offset require --category")
		}
		if category != "" {
			if category != "projects" && category != "issues" && category != "agents" && category != "skills" && category != "mcps" && category != "plugins" {
				return fmt.Errorf("--category must be projects, issues, agents, skills, mcps or plugins")
			}
			if limit < 1 || limit > 100 || offset < 0 {
				return fmt.Errorf("--limit must be 1–100 and --offset must be nonnegative")
			}
			query := url.Values{"category": {category}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
			suffix += "?" + query.Encode()
		}
	} else if cmd.Name() == "import" && (cmd.Flags().Changed("category") || cmd.Flags().Changed("limit") || cmd.Flags().Changed("offset")) {
		return fmt.Errorf("--category, --limit and --offset require --inspect")
	}
	if useDefault, _ := cmd.Flags().GetBool("default"); useDefault {
		suffix = "/package/default"
	}
	wsID, err := resolveWorkspaceArg(cmd, args)
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
	client.WorkspaceID = wsID
	if cmd.Name() == "import" && !inspect {
		response, err := importWorkspacePackage(cmd, client, wsID, body)
		if err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), response)
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	path := "/api/workspaces/" + url.PathEscape(wsID) + suffix
	var response json.RawMessage
	if cmd.Name() == "package" {
		err = client.GetJSON(ctx, path, &response)
	} else {
		var request any = body
		if cmd.Name() == "export" {
			request = selection
		}
		err = client.PostJSON(ctx, path, request, &response)
	}
	if err != nil {
		return err
	}
	if file, _ := cmd.Flags().GetString("file"); cmd.Name() == "export" && file != "" {
		out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if err := cli.PrintJSON(out, response); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return cli.PrintJSON(cmd.OutOrStdout(), response)
}

func readWorkspaceSelection(cmd *cobra.Command) (json.RawMessage, error) {
	if file, _ := cmd.Flags().GetString("selection-file"); file != "" {
		return readWorkspacePackageJSON(file)
	}
	return nil, nil
}

func buildWorkspaceImportBody(cmd *cobra.Command, file string) (map[string]any, error) {
	pkg, err := readWorkspacePackageJSON(file)
	if err != nil {
		return nil, err
	}
	selection, err := readWorkspaceSelection(cmd)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"package": pkg}
	if selection != nil {
		body["selection"] = selection
	}
	if activate, _ := cmd.Flags().GetBool("activate-plugins"); activate {
		token := resolveToken(cmd)
		if len(token) > 256 || !strings.HasPrefix(token, "mul_") {
			return nil, fmt.Errorf("--activate-plugins requires a member personal access token (mul_...)")
		}
		body["plugin_execution_token"] = token
	}
	return body, nil
}

func importWorkspacePackage(cmd *cobra.Command, client *cli.APIClient, wsID string, body map[string]any) (json.RawMessage, error) {
	activate, _ := cmd.Flags().GetBool("activate-plugins")
	fmt.Fprintln(cmd.ErrOrStderr(), "Replaces matching skill/MCP names and plugin keys in this workspace.")
	fmt.Fprintf(cmd.ErrOrStderr(), "Plugin activation: --activate-plugins=%t (default: false). Opt in with --activate-plugins.\n", activate)
	client.WorkspaceID = wsID
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	err := client.PostJSON(ctx, "/api/workspaces/"+url.PathEscape(wsID)+"/import", body, &response)
	if err != nil {
		if token, _ := body["plugin_execution_token"].(string); token != "" {
			// A proxy may echo the import request, including its credential.
			return nil, fmt.Errorf("workspace import: %s", strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
		}
		return nil, err
	}
	return response, nil
}
