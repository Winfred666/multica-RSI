package handler

import (
	"strings"
	"testing"
)

func TestOwningLeaderInWorkflowMovesOnlyByWorkflowCommands(t *testing.T) {
	protocol := strings.Join(strings.Fields(squadOperatingProtocolFor(true, leaderWorkflowCurrentDaemon)), " ")
	for _, move := range []string{"multica issue status <issue-id> in_review", "move the parent to `in_progress`"} {
		if strings.Contains(protocol, move) {
			t.Errorf("workflow leader instructions still carry built-in move %q", move)
		}
	}
	for _, want := range []string{
		"Own the parent issue status — through its project workflow.",
		"run that block's Step done command",
		"do it even when no comment asked you to",
		"The parent stays at its current step while members work",
	} {
		if !strings.Contains(protocol, want) {
			t.Errorf("workflow leader instructions missing %q", want)
		}
	}
}

// Without a workflow the owning leader keeps the built-in arc, so issues
// outside a handoff workflow see no change.
func TestOwningLeaderWithoutWorkflowKeepsBuiltInMoves(t *testing.T) {
	protocol := squadOperatingProtocolFor(true, leaderNoWorkflow)
	if strings.Contains(protocol, "through its project workflow") {
		t.Fatalf("owner protocol without a workflow must be the built-in form:\n%s", protocol)
	}
	compact := strings.Join(strings.Fields(protocol), " ")
	for _, want := range []string{"move the parent to `in_progress`", "multica issue status <issue-id> in_review"} {
		if !strings.Contains(compact, want) {
			t.Errorf("owner protocol without a workflow lost %q", want)
		}
	}
}

// A guest leader gets the prohibition whether or not the issue has a
// workflow: the workflow never widens status authority.
func TestGuestLeaderProtocolIgnoresWorkflow(t *testing.T) {
	if squadOperatingProtocolFor(false, leaderWorkflowCurrentDaemon) != squadOperatingProtocolFor(false, leaderNoWorkflow) || squadOperatingProtocolFor(false, leaderWorkflowLegacyDaemon) != squadOperatingProtocolFor(false, leaderNoWorkflow) {
		t.Fatal("guest leader protocol must not vary with the project workflow")
	}
	if strings.Contains(squadOperatingProtocolFor(false, leaderWorkflowCurrentDaemon), "Step done") {
		t.Fatal("guest leader protocol must not hand out the Step done move")
	}
}

// A current daemon's runtime brief states no built-in moves, so the protocol
// forbids none; only an older daemon's brief, which still states them, gets
// the override (MUL-8145).
func TestOwningLeaderInWorkflowOverridesBuiltInMovesOnlyForLegacyDaemons(t *testing.T) {
	current := strings.Join(strings.Fields(squadOperatingProtocolFor(true, leaderWorkflowCurrentDaemon)), " ")
	legacy := strings.Join(strings.Fields(squadOperatingProtocolFor(true, leaderWorkflowLegacyDaemon)), " ")
	for _, word := range []string{"in_progress", "in_review"} {
		if strings.Contains(current, word) {
			t.Errorf("current-daemon protocol names %q, which nothing in its brief asks for", word)
		}
	}
	if !strings.Contains(legacy, "This overrides the in_progress / in_review rules in your runtime instructions") {
		t.Errorf("legacy-daemon protocol must override the built-in moves:\n%s", legacy)
	}
	if !strings.Contains(legacy, strings.Join(strings.Fields(squadParentStatusOwnedInWorkflow), " ")) {
		t.Errorf("legacy-daemon protocol must keep the workflow responsibility it extends")
	}
}
