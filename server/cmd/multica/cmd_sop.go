package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var sopCmd = &cobra.Command{
	Use:   "sop",
	Short: "Register host procedures for daemon short circuit steering",
}

var sopRegisterCmd = &cobra.Command{
	Use:   "register <issue-id> <name>",
	Short: "Register a host procedure and advertise its inputs on an issue",
	Long: "Registers an executable in the same local profile as the target daemon. " +
		"Only names and input declarations are advertised in issue metadata. " +
		"The daemon receives /sop JSON comments through normal comment routing and runs the procedure before waking the agent. " +
		"The procedure receives issue_id, comment_id and inputs as JSON on stdin. Use --arg for fixed interpreter arguments. " +
		"Comment inputs never select executables or arguments. Hosting requires local member credentials.",
	Example: "  multica --profile host sop register R-42 inspect --executable /usr/bin/python3 --arg /opt/sops/inspect.py --input topic",
	Args:    exactArgs(2),
	RunE:    runSOPRegister,
}

func init() {
	sopRegisterCmd.Flags().String("executable", "", "Absolute path to the SOP executable or interpreter (required)")
	sopRegisterCmd.Flags().StringArray("arg", nil, "Fixed host argument, such as an absolute Python script path (repeatable)")
	sopRegisterCmd.Flags().StringSlice("input", nil, "Required input names; values are JSON (comma-separated or repeated)")
	sopCmd.AddCommand(sopRegisterCmd)
}

func sopLocalClient(cmd *cobra.Command) (*cli.APIClient, error) {
	if err := requireHumanLocalCommand("sop " + cmd.Name()); err != nil {
		return nil, err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if client.Token == "" || strings.HasPrefix(client.Token, cli.TaskTokenPrefix) {
		return nil, fmt.Errorf("SOP hosting requires a dedicated member profile, not an agent task token")
	}
	return client, nil
}

func runSOPRegister(cmd *cobra.Command, args []string) error {
	if !cli.SOPNamePattern.MatchString(args[1]) {
		return fmt.Errorf("invalid SOP name: use lowercase letters, digits, underscores or hyphens, starting with a letter")
	}
	executable, _ := cmd.Flags().GetString("executable")
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("--executable must be an absolute path")
	}
	if _, err := exec.LookPath(executable); err != nil {
		return fmt.Errorf("SOP executable: %w", err)
	}
	fixedArgs, _ := cmd.Flags().GetStringArray("arg")
	inputs, _ := cmd.Flags().GetStringSlice("input")
	seen := make(map[string]bool)
	for _, name := range inputs {
		if !cli.SOPNamePattern.MatchString(name) || seen[name] {
			return fmt.Errorf("invalid or duplicate input name %q", name)
		}
		seen[name] = true
	}
	client, err := sopLocalClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	issue, err := fetchIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	dir, err := cli.SOPStateDir(resolveProfile(cmd))
	if err != nil {
		return err
	}
	entries, err := cli.ReadSOPRegistry(dir)
	if err != nil {
		return err
	}
	entry := cli.SOPRegistration{ServerURL: client.BaseURL, WorkspaceID: client.WorkspaceID, IssueID: issue.ID, Name: args[1], Executable: executable, Args: fixedArgs, Inputs: inputs}
	updated := false
	for i, old := range entries {
		if old.ServerURL == entry.ServerURL && old.WorkspaceID == entry.WorkspaceID && old.IssueID == entry.IssueID && old.Name == entry.Name {
			entries[i], updated = entry, true
		}
	}
	if !updated {
		entries = append(entries, entry)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := cli.WriteSOPJSON(filepath.Join(dir, "registry.json"), entries); err != nil {
		return err
	}
	declaration, err := json.Marshal(map[string]any{"inputs": inputs})
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID)+"/metadata/sop."+entry.Name, map[string]string{"value": string(declaration)}, &result); err != nil {
		return fmt.Errorf("advertise SOP (local registration saved): %w", err)
	}
	return cli.PrintJSON(cmd.OutOrStdout(), entry)
}
