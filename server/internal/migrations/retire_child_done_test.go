package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRetireAutomaticChildWakeupsPreservesExplicitWaits(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("integration test requires DATABASE_URL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
CREATE TEMP TABLE issue_wakeup (id text, system_rule text, enabled boolean, next_fire_at timestamptz, disabled_at timestamptz, revision int) ON COMMIT DROP;
CREATE TEMP TABLE issue_wakeup_receipt (wakeup_id text, processed_at timestamptz) ON COMMIT DROP;
CREATE TEMP TABLE agent_task_queue (context jsonb, status text, completed_at timestamptz) ON COMMIT DROP;
INSERT INTO issue_wakeup VALUES ('automatic', 'child_done', true, now(), NULL, 1), ('explicit', NULL, true, now(), NULL, 1);
INSERT INTO issue_wakeup_receipt VALUES ('automatic', NULL), ('explicit', NULL);
INSERT INTO agent_task_queue VALUES ('{"wakeup_system":"child_done"}', 'queued', NULL), ('{"wakeup_system":"child_done"}', 'deferred', NULL), ('{"wakeup_system":"child_done"}', 'running', NULL), ('{}', 'queued', NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, readMigrationFile(t, "596_retire_child_done_system_wakeup.up.sql")); err != nil {
		t.Fatal(err)
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT
    (SELECT count(*)=1 FROM issue_wakeup WHERE id='automatic' AND NOT enabled AND next_fire_at IS NULL AND disabled_at IS NOT NULL AND revision=2)
AND (SELECT count(*)=1 FROM issue_wakeup WHERE id='explicit' AND enabled AND next_fire_at IS NOT NULL AND disabled_at IS NULL AND revision=1)
AND (SELECT count(*)=1 FROM issue_wakeup_receipt WHERE wakeup_id='automatic' AND processed_at IS NOT NULL)
AND (SELECT count(*)=1 FROM issue_wakeup_receipt WHERE wakeup_id='explicit' AND processed_at IS NULL)
AND (SELECT count(*)=2 FROM agent_task_queue WHERE status='cancelled' AND completed_at IS NOT NULL)
AND (SELECT count(*)=1 FROM agent_task_queue WHERE status='running' AND completed_at IS NULL)
AND (SELECT count(*)=1 FROM agent_task_queue WHERE status='queued' AND completed_at IS NULL)`).Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("retirement changed explicit waits or failed to retire automatic work: %v", err)
	}
}
