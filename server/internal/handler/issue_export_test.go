package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueSnapshotRestoreRoundTrip(t *testing.T) {
	storage := &mockStorage{files: map[string][]byte{}}
	h := *testHandler
	h.Storage = storage
	labelID := createTestIssueLabel(t, "snapshot-"+uuid.NewString()[:8])
	property := createTestProperty(t, map[string]any{"name": "Snapshot" + uuid.NewString()[:8], "type": "text"})
	number, err := h.Queries.IncrementIssueCounter(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	issueID := dbfx.Issue(t, "snapshot source", testutil.Cols{
		"number":      number,
		"description": "Read [notes](/api/attachments/PLACEHOLDER/download)",
		"status":      "done", "priority": "high", "metadata": `{"replay":true,"attempt":2}`,
		"properties":    fmt.Sprintf(`{%q:"retained"}`, property.ID),
		"assignee_type": "member", "assignee_id": testUserID,
		"start_date": "2026-10-01", "due_date": "2026-10-31",
	})
	dbfx.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": issueID, "label_id": labelID}, "issue_id = $1 AND label_id = $2", issueID, labelID)
	parent := dbfx.Comment(t, issueID, "root")
	child := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": parent, "resolved_at": testutil.Raw("now()"), "resolved_by_type": "member", "resolved_by_id": testUserID})
	tombstone := dbfx.Comment(t, issueID, "", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	dbfx.Comment(t, issueID, "reply to deleted root", testutil.Cols{"parent_id": tombstone})
	attachmentID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID, "comment_id": child,
		"uploader_type": "member", "uploader_id": testUserID, "filename": "notes.txt",
		"url": storage.ObjectURL("original.txt"), "content_type": "text/plain", "size_bytes": 5,
	})
	storage.files["original.txt"] = []byte("notes")
	if _, err := testPool.Exec(context.Background(), "UPDATE issue SET description = replace(description, 'PLACEHOLDER', $2) WHERE id = $1", issueID, attachmentID); err != nil {
		t.Fatal(err)
	}
	var snapshot IssueSnapshot
	testutil.Call(t, h.GetIssueSnapshot, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/snapshot", nil), "id", issueID)).Want(200).JSON(&snapshot)
	if len(snapshot.Comments) != 4 || len(snapshot.Attachments) != 1 || string(snapshot.Attachments[0].Data) != "notes" {
		t.Fatalf("incomplete snapshot: comments=%d attachments=%d", len(snapshot.Comments), len(snapshot.Attachments))
	}
	// A saved document must outlive its source rows and storage objects.
	testutil.Call(t, h.DeleteIssue, withURLParam(newRequest("DELETE", "/api/issues/"+issueID, nil), "id", issueID)).Want(204)
	storage.Delete(context.Background(), "original.txt")
	var restored IssueResponse
	testutil.Call(t, h.RestoreIssue, newRequest("POST", "/api/issues/restore", RestoreIssueRequest{Snapshot: snapshot})).Want(201).JSON(&restored)
	t.Cleanup(func() { deleteTestIssue(t, restored.ID) })
	if restored.ID == issueID || restored.Number == snapshot.Issue.Number || restored.Status != "backlog" || restored.AssigneeID != nil || restored.ParentIssueID != nil {
		t.Fatalf("restored identity/state: %+v", restored)
	}
	if restored.Metadata["replay"] != true || restored.Properties[property.ID] != "retained" || countIssueLabel(t, restored.ID, labelID) != 1 {
		t.Fatalf("metadata, property or label missing: %+v", restored)
	}
	if restored.StartDate == nil || *restored.StartDate != "2026-10-01" || restored.DueDate == nil || *restored.DueDate != "2026-10-31" {
		t.Fatal("dates lost")
	}
	var roundTrip IssueSnapshot
	testutil.Call(t, h.GetIssueSnapshot, withURLParam(newRequest("GET", "/api/issues/"+restored.ID+"/snapshot", nil), "id", restored.ID)).Want(200).JSON(&roundTrip)
	byContent := map[string]CommentResponse{}
	for _, c := range roundTrip.Comments {
		byContent[c.Content] = c
		if c.ID == parent || c.ID == child || c.SourceTaskID != nil || c.QuickActionID != nil || c.AuthorID != testUserID {
			t.Fatalf("stale comment identity: %+v", c)
		}
	}
	if len(roundTrip.Comments) != 4 || byContent["reply"].ParentID == nil || *byContent["reply"].ParentID != byContent["root"].ID || byContent["reply"].ResolvedAt == nil {
		t.Fatal("thread or resolution not restored")
	}
	if byContent[""].DeletedAt == nil || byContent["reply to deleted root"].ParentID == nil || *byContent["reply to deleted root"].ParentID != byContent[""].ID {
		t.Fatal("tombstone thread not restored")
	}
	a := roundTrip.Attachments[0]
	if a.ID == attachmentID || string(a.Data) != "notes" || a.CommentID == nil || *a.CommentID != byContent["reply"].ID {
		t.Fatalf("attachment not rebound: %+v", a.AttachmentResponse)
	}
	if strings.Contains(*restored.Description, attachmentID) || !strings.Contains(*restored.Description, a.ID) {
		t.Fatalf("attachment link not rewritten: %s", *restored.Description)
	}
	var tasks int
	if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", restored.ID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("restore launched execution: tasks=%d", tasks)
	}
}

func TestIssueRestoreRejectsInvalidSnapshotWithoutWrites(t *testing.T) {
	parent, child := uuid.NewString(), uuid.NewString()
	for _, tc := range []struct {
		name   string
		mutate func(*IssueSnapshot)
	}{
		{"version", func(s *IssueSnapshot) { s.Version = 2 }},
		{"missing attachment data", func(s *IssueSnapshot) {
			s.Attachments = []IssueSnapshotAttachment{{AttachmentResponse: AttachmentResponse{ID: uuid.NewString(), Filename: "missing.txt"}}}
		}},
		{"workspace", func(s *IssueSnapshot) { s.Issue.WorkspaceID = uuid.NewString() }},
		{"duplicate comment", func(s *IssueSnapshot) { s.Comments[1].ID = parent }},
		{"missing parent", func(s *IssueSnapshot) { v := uuid.NewString(); s.Comments[0].ParentID = &v }},
		{"cycle", func(s *IssueSnapshot) { s.Comments[0].ParentID = &child }},
		{"metadata", func(s *IssueSnapshot) { s.Issue.Metadata = map[string]any{"nested": map[string]any{"x": 1}} }},
		{"property", func(s *IssueSnapshot) { s.Issue.Properties = map[string]any{uuid.NewString(): "missing"} }},
		{"project", func(s *IssueSnapshot) { v := uuid.NewString(); s.Issue.ProjectID = &v }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title := "invalid snapshot " + uuid.NewString()
			snapshot := IssueSnapshot{Version: 1, Issue: IssueResponse{WorkspaceID: testWorkspaceID, Title: title, Priority: "none"}, Comments: []CommentResponse{{ID: parent, Content: "root", Type: "comment"}, {ID: child, ParentID: &parent, Content: "reply", Type: "comment"}}}
			tc.mutate(&snapshot)
			testutil.Call(t, testHandler.RestoreIssue, newRequest("POST", "/api/issues/restore", RestoreIssueRequest{Snapshot: snapshot})).Want(http.StatusBadRequest)
			if countIssuesWithTitle(t, title) != 0 {
				t.Fatal("invalid restore left an issue")
			}
		})
	}
}

func TestIssueRestoreRollsBackFilesAndRows(t *testing.T) {
	storage := &mockStorage{files: map[string][]byte{}}
	h := *testHandler
	h.Storage = storage
	title := "snapshot rollback " + uuid.NewString()
	snapshot := IssueSnapshot{Version: 1, Issue: IssueResponse{WorkspaceID: testWorkspaceID, Title: title, Priority: "none", Metadata: map[string]any{"large": strings.Repeat("x", 9000)}}, Attachments: []IssueSnapshotAttachment{{AttachmentResponse: AttachmentResponse{ID: uuid.NewString(), Filename: "test.txt"}, Data: []byte("test")}}}
	testutil.Call(t, h.RestoreIssue, newRequest("POST", "/api/issues/restore", RestoreIssueRequest{Snapshot: snapshot})).Want(400)
	if len(storage.files) != 0 || countIssuesWithTitle(t, title) != 0 {
		t.Fatalf("failed restore left files=%d or an issue", len(storage.files))
	}
}

func TestIssueSnapshotWorkspaceBoundaryAndMissingFile(t *testing.T) {
	h := *testHandler
	h.Storage = &mockStorage{}
	foreignWS := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Foreign", "slug": "snapshot-" + uuid.NewString(), "issue_prefix": "FSS"})
	foreign := dbfx.Issue(t, "foreign", testutil.Cols{"workspace_id": foreignWS})
	testutil.Call(t, h.GetIssueSnapshot, withURLParam(newRequest("GET", "/api/issues/"+foreign+"/snapshot", nil), "id", foreign)).Want(404)
	own := dbfx.Issue(t, "missing file")
	dbfx.Insert(t, "attachment", testutil.Cols{"workspace_id": testWorkspaceID, "issue_id": own, "uploader_type": "member", "uploader_id": testUserID, "filename": "missing.txt", "url": "https://cdn.example.com/missing.txt", "content_type": "text/plain", "size_bytes": 1})
	testutil.Call(t, h.GetIssueSnapshot, withURLParam(newRequest("GET", "/api/issues/"+own+"/snapshot", nil), "id", own)).Want(409)
}
