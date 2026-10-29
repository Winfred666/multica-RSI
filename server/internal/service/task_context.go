package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const IssueContextTokensMetadataKey = "context_tokens"

// Called inside the completion transaction so callback retries cannot append twice.
func appendTaskContextTokens(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, result []byte) (*db.SetIssueMetadataKeyRow, error) {
	if !task.IssueID.Valid {
		return nil, nil
	}
	agent, err := q.GetAgent(ctx, task.AgentID)
	if err != nil {
		return nil, err
	}
	issue, err := q.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: task.IssueID, WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	history := make(map[string][]int64)
	if encoded, ok := util.JSONObjectOrEmpty(issue.Metadata)[IssueContextTokensMetadataKey].(string); ok {
		if err := json.Unmarshal([]byte(encoded), &history); err != nil {
			return nil, fmt.Errorf("decode context token history: %w", err)
		}
	}
	if history == nil {
		history = make(map[string][]int64)
	}
	var payload protocol.TaskCompletedPayload
	if len(result) > 0 {
		if err := json.Unmarshal(result, &payload); err != nil {
			return nil, err
		}
	}
	agentID := util.UUIDToString(task.AgentID)
	history[agentID] = append(history[agentID], max(0, payload.ContextTokens))
	encoded, err := json.Marshal(history)
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(string(encoded))
	if err != nil {
		return nil, err
	}
	updated, err := q.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
		ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: IssueContextTokensMetadataKey, Value: value,
	})
	return &updated, err
}
