package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var attachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Work with attachments",
}

var attachmentDownloadCmd = &cobra.Command{
	Use:   "download <attachment-id>",
	Short: "Download an attachment to a local file",
	Long:  "Download an attachment by its ID to a local file.",
	Example: `  # Download an image attachment to the current directory
  $ multica attachment download abc123

  # Download to a directory inside the working directory (keep agent
  # downloads out of /tmp and other machine-shared paths, MUL-4252)
  $ multica attachment download abc123 -o ./attachments`,
	Args: exactArgs(1),
	RunE: runAttachmentDownload,
}

var attachmentUploadCmd = &cobra.Command{
	Use:   "upload <path>",
	Short: "Upload to the current issue/comment, or explicitly share an independent deliverable",
	Long: `Default uploads require MULTICA_ISSUE_ID and MULTICA_COMMENT_ID and belong to both.
Use --independent to leave ownership empty and soft-link to MULTICA_ISSUE_ID (required).
Use --independent --attachment-id to renew a shared deliverable, preserving its ID.
Use --task explicitly to attach a new upload to a chat task's reply.`,
	Example: `  $ multica attachment upload ./report.html
  $ multica attachment upload ./report.html --independent --attachment-id UUID
  $ multica attachment upload ./chart.png --task TASK_UUID`,
	Args: exactArgs(1),
	RunE: runAttachmentUpload,
}

var attachmentGetCmd = &cobra.Command{
	Use:   "get <attachment-id>",
	Short: "Get attachment metadata and download URLs",
	Args:  exactArgs(1),
	RunE:  runAttachmentGet,
}

var attachmentAttachCmd = &cobra.Command{
	Use:   "attach <attachment-id>",
	Short: "Attach a file to an issue, or soft-link an ownerless file",
	Args:  exactArgs(1),
	RunE:  runAttachmentLink,
}

var attachmentDetachCmd = &cobra.Command{
	Use:   "detach <attachment-id>",
	Short: "Detach a file from an issue, or remove its soft link, without deleting it",
	Args:  exactArgs(1),
	RunE:  runAttachmentLink,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)
	attachmentCmd.AddCommand(attachmentUploadCmd, attachmentGetCmd, attachmentAttachCmd, attachmentDetachCmd)
	for _, cmd := range []*cobra.Command{attachmentAttachCmd, attachmentDetachCmd} {
		cmd.Flags().String("issue", "", "Issue key or UUID (required)")
		cmd.Flags().Bool("independent", false, "Only update the issue's independent_artifacts metadata; requires an independent file")
	}

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Directory to save the downloaded file")
	attachmentUploadCmd.Flags().String("task", "", "Explicit chat task ID for a new upload")
	attachmentUploadCmd.Flags().String("attachment-id", "", "Ownerless attachment ID to renew")
	attachmentUploadCmd.Flags().Bool("independent", false, "Keep ownership empty and soft-link to the current issue")
	attachmentUploadCmd.MarkFlagsMutuallyExclusive("task", "attachment-id")
	attachmentUploadCmd.MarkFlagsMutuallyExclusive("task", "independent")
}

func runAttachmentUpload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	attachmentID, _ := cmd.Flags().GetString("attachment-id")
	independent, _ := cmd.Flags().GetBool("independent")
	issueID, commentID := os.Getenv("MULTICA_ISSUE_ID"), os.Getenv("MULTICA_COMMENT_ID")
	if taskID != "" && attachmentID != "" {
		return fmt.Errorf("--task and --attachment-id are mutually exclusive")
	}
	if taskID == "" && (issueID == "" || (!independent && commentID == "")) {
		return fmt.Errorf("upload requires MULTICA_ISSUE_ID and MULTICA_COMMENT_ID; --independent requires MULTICA_ISSUE_ID only")
	}
	if attachmentID != "" && !independent {
		return fmt.Errorf("--attachment-id requires --independent")
	}

	path := args[0]
	if isHTTPURL(path) {
		return fmt.Errorf("upload accepts a local file path, not a URL: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var att cli.AttachmentResponse
	if taskID != "" {
		att, err = client.UploadChatAttachment(ctx, data, path, taskID)
	} else if independent {
		att, err = client.UploadAttachment(ctx, data, path, attachmentID)
	} else {
		att, err = client.UploadCommentAttachment(ctx, data, path, issueID, commentID)
	}
	if err != nil {
		return fmt.Errorf("upload attachment: %w", err)
	}
	if independent {
		if _, err := updateIndependentArtifactLink(ctx, client, issueID, att.ID, "attach"); err != nil {
			return fmt.Errorf("attachment %s uploaded but soft-link failed: %w", att.ID, err)
		}
	}

	filename := filepath.Base(path)
	markdown := attachmentMarkdown(filename, att.ContentType, att.MarkdownURL)
	fmt.Fprintln(os.Stderr, "Uploaded:", filename)

	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":           att.ID,
		"filename":     filename,
		"markdown_url": att.MarkdownURL,
		"markdown":     markdown,
	})
}

// attachmentMarkdown renders the markdown snippet that makes an uploaded file
// visible in a body: images inline via `![...]( )`, every other file as a
// block-level attachment card via `!file[...]( )`. The label is escaped so a
// name like `report[v2].pdf` stays one label instead of truncating the
// snippet. Shared by `attachment upload` (prints it for the agent to paste)
// and `issue create --attachment` (appends it to the description).
func attachmentMarkdown(filename, contentType, markdownURL string) string {
	label := escapeMarkdownLabel(filename)
	if strings.HasPrefix(contentType, "image/") {
		return fmt.Sprintf("![%s](%s)", label, markdownURL)
	}
	return fmt.Sprintf("!file[%s](%s)", label, markdownURL)
}

// escapeMarkdownLabel escapes the metacharacters a markdown link/image label
// may not contain unescaped ([ ] ( ) and backslash), so a filename like
// `report[v2].pdf` stays a single valid label instead of truncating the
// snippet. Kept in sync with the renderers' unescape set
// (packages/ui/markdown/file-cards.ts).
func escapeMarkdownLabel(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`[`, `\[`,
		`]`, `\]`,
		`(`, `\(`,
		`)`, `\)`,
	).Replace(s)
}

func runAttachmentDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	// Fetch attachment metadata (includes signed download_url).
	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}

	downloadURL := strVal(att, "download_url")
	if downloadURL == "" {
		return fmt.Errorf("attachment has no download URL")
	}

	filename := filepath.Base(strVal(att, "filename"))
	if filename == "" || filename == "." {
		filename = args[0]
	}

	// Download the file content.
	data, err := client.DownloadFile(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	// Write to the output directory, creating it if needed so `-o` works
	// against a directory that does not exist yet (the help example's
	// `-o ./attachments` in a clean workdir).
	outputDir, _ := cmd.Flags().GetString("output-dir")
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	destPath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Print the absolute path so agents can reference the file.
	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = destPath
	}
	fmt.Fprintln(os.Stderr, "Downloaded:", abs)

	// Also print as JSON for --output json compatibility.
	return cli.PrintJSON(os.Stdout, map[string]any{
		"id":       strVal(att, "id"),
		"filename": filename,
		"path":     abs,
		"size":     strVal(att, "size_bytes"),
	})
}

func runAttachmentGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}
	return cli.PrintJSON(os.Stdout, att)
}

func runAttachmentLink(cmd *cobra.Command, args []string) error {
	issue, _ := cmd.Flags().GetString("issue")
	if issue == "" {
		return fmt.Errorf("--issue is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	issueRef, err := resolveIssueRef(ctx, client, issue)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	independent, _ := cmd.Flags().GetBool("independent")
	var att map[string]any
	if err := client.GetJSON(ctx, "/api/attachments/"+args[0], &att); err != nil {
		return fmt.Errorf("get attachment: %w", err)
	}
	dependent := false
	for _, field := range []string{"issue_id", "comment_id", "chat_session_id", "chat_message_id"} {
		if att[field] != nil && att[field] != "" {
			dependent = true
		}
	}
	if independent && dependent {
		return fmt.Errorf("--independent requires an independent attachment")
	}
	if !independent && cmd.Name() == "attach" && dependent {
		return fmt.Errorf("attachment already has an owner; detach it first")
	}
	if !independent && cmd.Name() == "detach" && !dependent {
		return fmt.Errorf("attachment is already independent")
	}
	attachmentID := strVal(att, "id")
	if attachmentID == "" {
		return fmt.Errorf("attachment response missing id")
	}
	if !independent {
		var result map[string]any
		if err := client.PostJSON(ctx, "/api/attachments/"+attachmentID+"/"+cmd.Name(), map[string]any{"issue_id": issueRef.ID}, &result); err != nil {
			return fmt.Errorf("%s attachment: %w", cmd.Name(), err)
		}
		action := "detach"
		if cmd.Name() == "detach" {
			action = "attach"
		}
		if _, err := updateIndependentArtifactLink(ctx, client, issueRef.ID, attachmentID, action); err != nil {
			return fmt.Errorf("ownership changed but independent link update failed: %w", err)
		}
		return cli.PrintJSON(os.Stdout, result)
	}
	metadata, err := updateIndependentArtifactLink(ctx, client, issueRef.ID, attachmentID, cmd.Name())
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, metadata)
}

func updateIndependentArtifactLink(ctx context.Context, client *cli.APIClient, issueID, attachmentID, action string) (map[string]any, error) {
	var result map[string]any
	path := "/api/issues/" + issueID + "/metadata"
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("get metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	var ids []string
	if value, exists := metadata["independent_artifacts"]; exists {
		raw, ok := value.(string)
		if !ok || json.Unmarshal([]byte(raw), &ids) != nil || ids == nil {
			return nil, fmt.Errorf("independent_artifacts must be a JSON-string ID list")
		}
	}
	linked := make([]string, 0, len(ids)+1)
	seen := make(map[string]bool)
	if action == "attach" {
		ids = append(ids, attachmentID)
	}
	for _, id := range ids {
		if seen[id] || (action == "detach" && id == attachmentID) {
			continue
		}
		seen[id] = true
		linked = append(linked, id)
	}
	encoded, err := json.Marshal(linked)
	if err != nil {
		return nil, err
	}
	if err := client.PutJSON(ctx, path+"/independent_artifacts", map[string]any{"value": string(encoded)}, &result); err != nil {
		return nil, fmt.Errorf("set independent_artifacts: %w", err)
	}
	metadata, _ = result["metadata"].(map[string]any)
	return metadata, nil
}
