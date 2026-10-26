package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRemoveOnboardingSourceAnswers(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires DATABASE_URL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// The temporary table shadows the account table for this connection only.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE "user" (onboarding_questionnaire jsonb) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ stored, want string }{
		{`{"source":["other"],"source_other":"old answer","source_skipped":false,"role":"engineer","use_case":["ship_code"],"version":2}`, `{"role":"engineer","use_case":["ship_code"],"version":2}`},
		{`{"source":"search","source_skipped":true}`, `{}`},
		{`{"role":"designer","future_field":42}`, `{"role":"designer","future_field":42}`},
		{`{}`, `{}`},
		{`null`, `null`},
	} {
		t.Run(tc.stored, func(t *testing.T) {
			if _, err := tx.Exec(ctx, `INSERT INTO "user" VALUES ($1::jsonb)`, tc.stored); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := tx.Exec(ctx, readMigrationFile(t, "599_remove_onboarding_source_answers.up.sql")); err != nil {
					t.Fatal(err)
				}
			}
			var matches bool
			if err := tx.QueryRow(ctx, `SELECT onboarding_questionnaire = $1::jsonb FROM "user"`, tc.want).Scan(&matches); err != nil {
				t.Fatal(err)
			}
			if !matches {
				t.Fatalf("source cleanup did not produce %s", tc.want)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM "user"`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
