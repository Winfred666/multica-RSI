package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestRunAttachmentDownloadInterruptedBodyPreservesTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/attachments/att-interrupted":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"filename": "report.txt", "download_url": "/download",
			})
		case "/download":
			w.Header().Set("Content-Length", "71")
			_, _ = io.WriteString(w, "payload")
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	outputDir := t.TempDir()
	dest := filepath.Join(outputDir, "report.txt")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newAttachmentDownloadTestCmd()
	_ = cmd.Flags().Set("output-dir", outputDir)
	out, err := captureStdout(t, func() error {
		return runAttachmentDownload(cmd, []string{"att-interrupted"})
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) || cli.ExitCodeFor(err) != cli.ExitNetwork {
		t.Fatalf("expected network error preserving unexpected EOF, got %v (exit %d)", err, cli.ExitCodeFor(err))
	}
	if !strings.HasPrefix(err.Error(), "download file: ") || out != "" {
		t.Errorf("error context or failure output changed: err=%v stdout=%q", err, out)
	}
	data, readErr := os.ReadFile(dest)
	if readErr != nil || string(data) != "original" {
		t.Fatalf("target changed after interrupted download: data=%q err=%v", data, readErr)
	}
}

func newAttachmentDownloadTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "download"}
	cmd.Flags().StringP("output-dir", "o", ".", "")
	return cmd
}

func TestRunAttachmentDownloadWritesBasenameIntoOutputDir(t *testing.T) {
	const attachmentID = "att-123"
	const fileBody = "attachment body"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/"+attachmentID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           attachmentID,
				"filename":     "../report.txt",
				"download_url": "/downloads/report.txt",
				"size_bytes":   "15",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/downloads/report.txt":
			if r.Header.Get("Authorization") == "" {
				t.Fatalf("relative download missing auth header")
			}
			_, _ = w.Write([]byte(fileBody))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	// A task-scoped mat_ token so the test also runs inside an agent workdir,
	// where a daemon task marker makes newAPIClient reject the helper's
	// default non-mat_ token before the download logic is reached.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	outputDir := t.TempDir()
	cmd := newAttachmentDownloadTestCmd()
	_ = cmd.Flags().Set("output-dir", outputDir)

	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error { return runAttachmentDownload(cmd, []string{attachmentID}) })
	errOut := stderr.read()
	if err != nil {
		t.Fatalf("runAttachmentDownload: %v", err)
	}

	dest := filepath.Join(outputDir, "report.txt")
	data, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read downloaded file: %v", readErr)
	}
	if string(data) != fileBody {
		t.Fatalf("downloaded body = %q, want %q", data, fileBody)
	}
	if strings.Contains(out, "../") {
		t.Fatalf("stdout path should use sanitized basename, got %q", out)
	}
	if !strings.Contains(out, `"filename": "report.txt"`) || !strings.Contains(out, dest) {
		t.Fatalf("stdout = %q, want JSON with sanitized file path", out)
	}
	if !strings.Contains(errOut, "Downloaded:") || !strings.Contains(errOut, dest) {
		t.Fatalf("stderr = %q, want downloaded path", errOut)
	}
}

// TestRunAttachmentDownloadCreatesMissingOutputDir pins that `-o` works
// against a directory that does not exist yet — the shape the command's own
// help example (`-o ./attachments`) produces in a clean workdir (MUL-5696).
func TestRunAttachmentDownloadCreatesMissingOutputDir(t *testing.T) {
	const attachmentID = "att-456"
	const fileBody = "nested dir body"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/"+attachmentID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           attachmentID,
				"filename":     "shot.png",
				"download_url": "/downloads/shot.png",
				"size_bytes":   "15",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/downloads/shot.png":
			_, _ = w.Write([]byte(fileBody))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	outputDir := filepath.Join(t.TempDir(), "attachments", "images")
	cmd := newAttachmentDownloadTestCmd()
	_ = cmd.Flags().Set("output-dir", outputDir)

	stderr := captureStderr(t)
	_, err := captureStdout(t, func() error { return runAttachmentDownload(cmd, []string{attachmentID}) })
	_ = stderr.read()
	if err != nil {
		t.Fatalf("runAttachmentDownload with missing output dir: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(outputDir, "shot.png"))
	if readErr != nil {
		t.Fatalf("read downloaded file: %v", readErr)
	}
	if string(data) != fileBody {
		t.Fatalf("downloaded body = %q, want %q", data, fileBody)
	}
}

func newAttachmentUploadTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "upload"}
	cmd.Flags().String("task", "", "")
	cmd.Flags().String("attachment-id", "", "")
	return cmd
}

func TestRunAttachmentUploadSendsTaskIDAndPrintsContract(t *testing.T) {
	const taskID = "task-abc"
	var gotTaskID string
	var gotFile string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/upload-file" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotTaskID = r.FormValue("task_id")
		if f, _, err := r.FormFile("file"); err == nil {
			_ = f.Close()
			gotFile = "present"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "att-999",
			"filename":     "chart.png",
			"content_type": "image/png",
			"url":          "https://cdn.example/chart.png",
			"download_url": "https://signed.example/chart.png?sig=x",
			"markdown_url": "https://public.example/api/attachments/att-999/download",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	// An agent upload always carries a task-scoped mat_ token; set one so the
	// daemon-managed-context gate in newAPIClient admits the request.
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\nbytes"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	cmd := newAttachmentUploadTestCmd()
	_ = cmd.Flags().Set("task", taskID)

	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{imgPath}) })
	if err != nil {
		t.Fatalf("runAttachmentUpload: %v", err)
	}
	if gotTaskID != taskID {
		t.Fatalf("server task_id = %q, want %q", gotTaskID, taskID)
	}
	if gotFile != "present" {
		t.Fatalf("server did not receive a file part")
	}
	// Output contract: id, markdown_url, and a ready-to-paste markdown snippet.
	if !strings.Contains(out, `"id": "att-999"`) {
		t.Fatalf("stdout missing id: %q", out)
	}
	if !strings.Contains(out, `"markdown_url": "https://public.example/api/attachments/att-999/download"`) {
		t.Fatalf("stdout missing markdown_url: %q", out)
	}
	// Image content type → image markdown so it renders inline.
	if !strings.Contains(out, `![chart.png](https://public.example/api/attachments/att-999/download)`) {
		t.Fatalf("stdout missing image markdown snippet: %q", out)
	}
}

// TestRunAttachmentUploadNonImageUsesFileCardMarkdown covers the content-type
// branch: a non-image file emits the block-level `!file[...]( )` card snippet,
// not image `![...]` markdown.
func TestRunAttachmentUploadNonImageUsesFileCardMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "att-doc",
			"filename":     "report.pdf",
			"content_type": "application/pdf",
			"url":          "https://cdn.example/report.pdf",
			"markdown_url": "https://public.example/api/attachments/att-doc/download",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	dir := t.TempDir()
	docPath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(docPath, []byte("%PDF-1.4 bytes"), 0o644); err != nil {
		t.Fatalf("write temp doc: %v", err)
	}

	cmd := newAttachmentUploadTestCmd()
	_ = cmd.Flags().Set("task", "task-abc")

	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{docPath}) })
	if err != nil {
		t.Fatalf("runAttachmentUpload: %v", err)
	}
	// Non-image uses the block-level file-card snippet, never image markdown.
	if !strings.Contains(out, `!file[report.pdf](https://public.example/api/attachments/att-doc/download)`) {
		t.Fatalf("stdout missing file-card markdown snippet: %q", out)
	}
	if strings.Contains(out, `![report.pdf]`) {
		t.Fatalf("non-image must not use image markdown: %q", out)
	}
	if !strings.Contains(out, `"markdown_url": "https://public.example/api/attachments/att-doc/download"`) {
		t.Fatalf("stdout missing markdown_url: %q", out)
	}
}

// TestRunAttachmentUploadEscapesFilename covers filenames carrying markdown
// label metacharacters (`[`, `]`): the snippet must escape them so the label
// does not truncate — otherwise the card fails to render and the standalone
// attachment is hidden by the referenced URL, showing nothing.
func TestRunAttachmentUploadEscapesFilename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "att-esc",
			"filename":     "a]b.pdf",
			"content_type": "application/pdf",
			"markdown_url": "https://public.example/api/attachments/att-esc/download",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	dir := t.TempDir()
	docPath := filepath.Join(dir, "a]b.pdf")
	if err := os.WriteFile(docPath, []byte("%PDF-1.4 bytes"), 0o644); err != nil {
		t.Fatalf("write temp doc: %v", err)
	}

	cmd := newAttachmentUploadTestCmd()
	_ = cmd.Flags().Set("task", "task-abc")

	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{docPath}) })
	if err != nil {
		t.Fatalf("runAttachmentUpload: %v", err)
	}
	// The `]` in the filename must be escaped inside the label.
	if !strings.Contains(out, `!file[a\\]b.pdf](https://public.example/api/attachments/att-esc/download)`) {
		t.Fatalf("stdout missing escaped file-card snippet: %q", out)
	}
}

func TestRunAttachmentUploadOwnerlessAndRenewal(t *testing.T) {
	for _, renewalID := range []string{"", "att-current"} {
		t.Run("attachment-id="+renewalID, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/upload-file" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				want := map[string][]string{}
				if renewalID != "" {
					want["attachment_id"] = []string{renewalID}
				}
				if !reflect.DeepEqual(r.MultipartForm.Value, want) {
					t.Errorf("multipart fields = %v, want %v (no ambient owners)", r.MultipartForm.Value, want)
				}
				f, _, err := r.FormFile("file")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				data, _ := io.ReadAll(f)
				if string(data) != "current bytes" {
					t.Errorf("file = %q", data)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "att-current", "markdown_url": "/download"})
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_test-token")
			t.Setenv("MULTICA_TASK_ID", "ambient-task")
			t.Setenv("MULTICA_ISSUE_ID", "ambient-issue")
			path := filepath.Join(t.TempDir(), "report.txt")
			if err := os.WriteFile(path, []byte("current bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := newAttachmentUploadTestCmd()
			_ = cmd.Flags().Set("attachment-id", renewalID)
			out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) })
			if err != nil || !strings.Contains(out, `"id": "att-current"`) {
				t.Fatalf("upload: %v, stdout=%s", err, out)
			}
		})
	}
}

func TestRunAttachmentSoftLinksOnlyUpdateMetadata(t *testing.T) {
	metadata := map[string]any{"independent_artifacts": `["other","att-1","other","att-1"]`, "keep": "untouched"}
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/att-1":
			// Hard detach clears owners but retains the producing task.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "att-1", "issue_id": nil, "comment_id": nil,
				"chat_session_id": nil, "chat_message_id": nil, "task_id": "producing-task",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID+"/metadata":
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": metadata})
		case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+testIssueUUID+"/metadata/independent_artifacts":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 {
				t.Errorf("only the value should be sent: %v", body)
			}
			metadata["independent_artifacts"] = body["value"]
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": metadata})
		default:
			t.Errorf("soft link must not mutate ownership or delete bytes: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	for _, step := range []struct{ action, want string }{
		{"attach", `["other","att-1"]`}, {"attach", `["other","att-1"]`},
		{"detach", `["other"]`}, {"detach", `["other"]`},
	} {
		cmd := &cobra.Command{Use: step.action}
		cmd.Flags().String("issue", testIssueUUID, "")
		cmd.Flags().Bool("soft", true, "")
		_, err := captureStdout(t, func() error { return runAttachmentLink(cmd, []string{"att-1"}) })
		if err != nil || metadata["independent_artifacts"] != step.want || metadata["keep"] != "untouched" {
			t.Fatalf("%s: %v, metadata=%v, requests=%v", step.action, err, metadata, methods)
		}
	}
}

func TestRunAttachmentDownloadRequiresDownloadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/attachments/att-no-url" {
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "att-no-url",
			"filename": "missing.txt",
		})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	cmd := newAttachmentDownloadTestCmd()
	if err := runAttachmentDownload(cmd, []string{"att-no-url"}); err == nil || !strings.Contains(err.Error(), "no download URL") {
		t.Fatalf("runAttachmentDownload error = %v, want missing download URL", err)
	}
}
