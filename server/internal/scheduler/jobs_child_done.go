package scheduler

import (
	"context"
	"time"
)

type ChildEventSweeper interface {
	SweepChildEvents(context.Context) error
}

// ChildEventSweepJob processes sub-issue changes that a write recorded but did
// not finish processing (a crash, a deploy, a transient database error); the
// rows themselves are the durable queue for explicit children_done conditions.
func ChildEventSweepJob(sweeper ChildEventSweeper) JobSpec {
	return JobSpec{
		Name: "issue_child_event_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, sweeper.SweepChildEvents(ctx)
		},
	}
}
