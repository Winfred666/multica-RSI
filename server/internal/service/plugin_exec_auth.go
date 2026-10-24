package service

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type PluginExecutionOwner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Only metadata leaves the credential store; the supplied PAT is write-only.
type PluginExecutionStatus struct {
	Configured bool                  `json:"configured"`
	Enabled    bool                  `json:"enabled"`
	RunAs      *PluginExecutionOwner `json:"run_as"`
}

func (s *PluginService) PluginExecutionStatus(ctx context.Context, workspaceID pgtype.UUID) (PluginExecutionStatus, error) {
	row, err := s.Queries.GetWorkspacePluginExecutionStatus(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginExecutionStatus{}, nil
	}
	if err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "load workspace execution settings", Err: err}
	}
	status := PluginExecutionStatus{Configured: true, Enabled: row.Enabled && s.Secrets != nil && s.ServerURL != ""}
	if row.UserID.Valid {
		status.RunAs = &PluginExecutionOwner{ID: uuidString(row.UserID), Name: row.Name.String}
	}
	return status, nil
}

func validatePluginExecutionPAT(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, token string) (db.PersonalAccessToken, error) {
	if !strings.HasPrefix(token, "mul_") || len(token) > 256 {
		return db.PersonalAccessToken{}, pluginErrf(PluginErrorInvalid, "supply a personal access token authorized by its owner")
	}
	pat, err := queries.GetPersonalAccessTokenByHash(ctx, auth.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return pat, pluginErrf(PluginErrorForbidden, "the supplied PAT is invalid, expired or revoked")
	}
	if err != nil {
		return pat, &PluginError{Kind: PluginErrorUnavailable, Message: "validate plugin execution credential", Err: err}
	}
	_, err = queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: pat.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return pat, pluginErrf(PluginErrorForbidden, "the PAT owner must be a member of this workspace")
	}
	if err != nil {
		return pat, &PluginError{Kind: PluginErrorUnavailable, Message: "check plugin execution membership", Err: err}
	}
	return pat, nil
}

func (s *PluginService) SetPluginExecution(ctx context.Context, workspaceID pgtype.UUID, token string) (PluginExecutionStatus, error) {
	if s.Secrets == nil || s.ServerURL == "" {
		return PluginExecutionStatus{}, pluginErrf(PluginErrorUnavailable, "hosted execution requires MULTICA_PLUGIN_SECRET_KEY and MULTICA_PUBLIC_URL")
	}
	token = strings.TrimSpace(token)
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "begin execution settings update", Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.Queries.WithTx(tx)
	// Workspace deletion holds FOR UPDATE before clearing credentials.
	if _, err := queries.LockWorkspaceForPluginWrite(ctx, workspaceID); err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "lock plugin workspace", Err: err}
	}
	pat, err := validatePluginExecutionPAT(ctx, queries, workspaceID, token)
	if err != nil {
		return PluginExecutionStatus{}, err
	}
	sealed, err := s.Secrets.Seal([]byte(token))
	if err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "encrypt execution credential", Err: err}
	}
	if err := queries.SetWorkspacePluginExecution(ctx, db.SetWorkspacePluginExecutionParams{WorkspaceID: workspaceID, PatID: pat.ID, Ciphertext: sealed}); err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "store execution credential", Err: err}
	}
	if err := tx.Commit(ctx); err != nil {
		return PluginExecutionStatus{}, &PluginError{Kind: PluginErrorUnavailable, Message: "commit execution settings", Err: err}
	}
	return s.PluginExecutionStatus(ctx, workspaceID)
}

func (s *PluginService) DeletePluginExecution(ctx context.Context, workspaceID pgtype.UUID) error {
	// The PAT belongs to its supplier; disconnecting does not revoke other uses.
	if err := s.Queries.DeleteWorkspacePluginExecution(ctx, workspaceID); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "remove execution credential", Err: err}
	}
	return nil
}

func (s *PluginService) executionToken(ctx context.Context, installation db.PluginInstallation) (string, error) {
	credential, err := s.Queries.GetWorkspacePluginExecution(ctx, installation.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pluginErrf(PluginErrorForbidden, "hosted execution is disabled; supply a workspace PAT in Settings > Plugins")
	}
	if err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "load execution credential", Err: err}
	}
	if s.Secrets == nil || s.ServerURL == "" {
		return "", pluginErrf(PluginErrorUnavailable, "hosted execution requires MULTICA_PLUGIN_SECRET_KEY and MULTICA_PUBLIC_URL")
	}
	token, err := s.Secrets.Open(credential.Ciphertext)
	if err != nil {
		return "", &PluginError{Kind: PluginErrorUnavailable, Message: "decrypt execution credential", Err: err}
	}
	pat, err := validatePluginExecutionPAT(ctx, s.Queries, installation.WorkspaceID, string(token))
	if err != nil {
		return "", err
	}
	if pat.ID != credential.PatID {
		return "", pluginErrf(PluginErrorForbidden, "workspace execution credential is invalid")
	}
	return string(token), nil
}
