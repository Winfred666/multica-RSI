package handler

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) installWorkspaceHarness(ctx context.Context, tx pgx.Tx, workspaceID, userID pgtype.UUID, keepContext bool) error {
	p, err := defaultWorkspacePackage()
	if err != nil {
		return err
	}
	if keepContext {
		p.SystemPrompt = nil
	}
	if !h.pluginsV1Enabled(ctx) {
		p.Plugins = []PackagePlugin{}
	}
	_, err = h.importWorkspacePackage(ctx, tx, workspaceID, userID, p, false)
	return err
}
