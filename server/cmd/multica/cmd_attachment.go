package main

import (
	"context"
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
	Short: "Upload a file or replace an attachment",
	Long: `Upload a local file and print its ID and ready-to-paste markdown.

When present, MULTICA_TASK_ID binds a new upload to the current chat reply;
--task overrides it. A task is not required.

Pass --attachment-id to replace a file. The server decides whether to renew
it or preserve the previous content with a new ID. Use the returned ID and
markdown_url for subsequent operations.`,
	Example: `  $ multica attachment upload ./chart.png
  $ multica attachment upload ./preview.html --attachment-id <id> --output json`,
	Args: exactArgs(1),
	RunE: runAttachmentUpload,
}

var attachmentGetCmd = &cobra.Command{
	Use:   "get <attachment-id>",
	Short: "Get attachment metadata",
	Args:  exactArgs(1),
	RunE:  runAttachmentGet,
}

func init() {
	attachmentCmd.AddCommand(attachmentDownloadCmd)
	attachmentCmd.AddCommand(attachmentUploadCmd)
	attachmentCmd.AddCommand(attachmentGetCmd)

	attachmentDownloadCmd.Flags().StringP("output-dir", "o", ".", "Directory to save the downloaded file")
	attachmentUploadCmd.Flags().String("task", "", "Chat task id to attach to (defaults to MULTICA_TASK_ID)")
	attachmentUploadCmd.Flags().String("attachment-id", "", "Attachment ID whose content to replace (the server may return a new ID)")
	attachmentUploadCmd.Flags().String("output", "json", "Output format: json")
	attachmentGetCmd.Flags().String("output", "json", "Output format: json")
}

func runAttachmentUpload(cmd *cobra.Command, args []string) error {
	attachmentID, _ := cmd.Flags().GetString("attachment-id")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	if taskID == "" {
		taskID = client.TaskID
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

	att, err := client.UploadAttachment(ctx, data, path, taskID, attachmentID)
	if err != nil {
		return fmt.Errorf("upload attachment: %w", err)
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
