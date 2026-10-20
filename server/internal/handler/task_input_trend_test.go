package handler

import "testing"

func TestAttachTaskInputTrends(t *testing.T) {
	run := func(id, agent, status string, input int64) AgentTaskResponse {
		return AgentTaskResponse{ID: id, AgentID: agent, Status: status, CreatedAt: "2026-01-0" + id,
			Usage: []TaskUsageData{{InputTokens: input, CacheReadTokens: 200, CacheWriteTokens: 100, OutputTokens: 9000}}}
	}
	rows := []AgentTaskResponse{
		run("6", "a", "completed", 700),
		run("5", "a", "completed", 700),
		run("4", "a", "failed", 1),
		{ID: "missing", AgentID: "a", Status: "completed", CreatedAt: "2026-01-03"},
		run("3", "b", "completed", 10),
		run("2", "a", "completed", 1700),
		run("1", "a", "completed", 1200),
		run("7", "a", "running", 1),
	}
	attachTaskInputTrends(rows)
	for index, want := range map[int]TaskInputTrend{
		0: {CurrentContext: 1000, ContextCompressionCount: 1},
		1: {CurrentContext: 1000, ContextCompressionCount: 1},
		4: {CurrentContext: 310},
		5: {CurrentContext: 2000},
		6: {CurrentContext: 1500},
	} {
		if got := rows[index].InputTrend; got == nil || *got != want {
			t.Errorf("run %s: got %+v, want %+v", rows[index].ID, got, want)
		}
	}
	for _, index := range []int{2, 3, 7} {
		if rows[index].InputTrend != nil {
			t.Errorf("run %s should have no input trend", rows[index].ID)
		}
	}
	if rows[0].ID != "6" {
		t.Fatal("changed response ordering")
	}
}
