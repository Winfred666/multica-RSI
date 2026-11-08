package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/spf13/cobra"
)

var remoteSOPCmd = &cobra.Command{
	Use:     "remote-sop <remote-issue-id> <SOP-name> [args...]",
	Short:   "Run a remote issue's SOP and print its execution-log output",
	Long:    "Run python3 <skill>/wrapper.py on the issue assignee's host. Try --help as the SOP argument first. Arguments pass through unchanged; results come from the execution log.",
	Example: "  multica remote-sop R-42 multica-subissue-distribute --help",
	RunE:    runRemoteSOP,
}

func init() { remoteSOPCmd.Flags().SetInterspersed(false) }

func runRemoteSOP(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: %s", cmd.UseLine())
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(args[0]), &issue); err != nil {
		return err
	}
	if strVal(issue, "assignee_type") != "agent" || strVal(issue, "assignee_id") == "" {
		return fmt.Errorf("remote issue must have an agent assignee")
	}
	issuePath := "/api/issues/" + url.PathEscape(strVal(issue, "id"))
	argv := append([]string{}, args[2:]...)
	encoded, err := json.Marshal(argv)
	if err != nil {
		return err
	}
	content := "/note !sop " + args[1] + " " + string(encoded)
	var comment struct {
		ID string `json:"id"`
	}
	if err := client.PostJSON(ctx, issuePath+"/comments", map[string]string{"content": content}, &comment); err != nil {
		return err
	}
	for {
		var runs []map[string]any
		if err := client.GetJSON(ctx, issuePath+"/task-runs", &runs); err != nil {
			return err
		}
		for _, run := range runs {
			id := strVal(run, "id")
			if id != comment.ID {
				continue
			}
			var messages []protocol.TaskMessagePayload
			if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(id)+"/messages", &messages); err != nil {
				return err
			}
			for _, message := range messages {
				if message.CallID != comment.ID || message.Type != "tool_result" || message.Tool != "remote-sop" {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s]\n%s", issueDisplayKey(issue), message.Output)
				if message.Content != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), message.Content)
				}
				if code, ok := message.Input["exit_code"].(float64); ok && code != 0 {
					return fmt.Errorf("remote SOP exited with code %g", code)
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
