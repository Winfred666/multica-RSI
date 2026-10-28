package execenv

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/runtimeapps"
)

func BuildOnBehalfOfBlock(name, email string) string {
	safeName := sanitizeNameForBriefMarkdown(name)
	if safeName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## On Behalf Of\n\n")
	if safeEmail := sanitizeEmailForBrief(email); safeEmail != "" {
		fmt.Fprintf(&b, "You are acting on behalf of **%s** (%s). ", safeName, safeEmail)
	} else {
		fmt.Fprintf(&b, "You are acting on behalf of **%s**. ", safeName)
	}
	b.WriteString("Apply any person-specific privacy or access rules in your instructions to this person. Your Multica credentials and access remain scoped to the runtime owner; do not assume this person can access everything you can.\n\n")
	return b.String()
}

func BuildConnectedAppsBlock(apps []runtimeapps.ConnectedApp) string {
	if len(apps) == 0 {
		return ""
	}
	var b strings.Builder
	var lines strings.Builder
	for _, app := range apps {
		serverName := sanitizeBriefCodeToken(app.ServerName)
		toolkitSlug := sanitizeBriefCodeToken(app.ToolkitSlug)
		if serverName == "" || toolkitSlug == "" {
			continue
		}
		name := sanitizeNameForBriefMarkdown(app.ToolkitName)
		if name == "" {
			name = sanitizeNameForBriefMarkdown(runtimeapps.DisplayNameForToolkitSlug(toolkitSlug))
		}
		if name == "" {
			name = toolkitSlug
		}
		fmt.Fprintf(&lines, "- %s (`%s`) via MCP server `%s`\n", name, toolkitSlug, serverName)
	}
	if lines.Len() == 0 {
		return ""
	}
	b.WriteString("## Connected Apps\n\n")
	b.WriteString(lines.String())
	b.WriteString("\nUse the listed MCP server when the task asks to read or act in one of these apps.\n\n")
	return b.String()
}

func sanitizeBriefCodeToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return ""
	}
	return s
}

const SessionContinuityNoticeIssue = "## Session Continuity Notice\n\n" +
	"This run was meant to continue an earlier conversation, but that provider session could not be restored, and this run does not continue it. The issue and its full comment history are unaffected — that record is the authoritative version of this conversation, and reading it (which your workflow already requires) reconstructs it. What is gone is your own working memory from the turns that did not come back: what you already tried, what you ruled out, and how far you had got. Re-derive what you need instead of assuming it, and do not claim continuity the record cannot back up. Do not open your reply by announcing this — raise it only where it actually matters, such as when the user refers to reasoning you never wrote down.\n\n"

const SessionContinuityNoticeChannelHistory = "## Session Continuity Notice\n\n" +
	"This run was meant to continue an earlier conversation, but that provider session could not be restored, and this run does not continue it. The channel conversation itself is unaffected — read it back with `multica chat history` / `multica chat thread` before acting, and treat what you find there as the authoritative version. What is gone is your own working memory from the turns that did not come back: what you already tried, what you ruled out, and how far you had got. Re-derive what you need instead of assuming it. Do not open your reply by announcing this — raise it only where it actually matters.\n\n"

const SessionContinuityNoticeChatTranscript = "## Session Continuity Notice\n\n" +
	"This run was meant to continue an earlier conversation, but that provider session could not be restored, and this run does not continue it. The conversation itself is unaffected — Multica stored it, and you can read it back with `multica chat history` before acting; treat what you find there as the authoritative version. What is gone is your own working memory from the turns that did not come back: what you already tried, what you ruled out, and how far you had got. Re-derive what you need instead of assuming it. Do not open your reply by announcing this — raise it only where it actually matters.\n\n"

const SessionContinuityNoticeUnrecoverable = "## Session Continuity Notice\n\n" +
	"This run was meant to continue an earlier conversation, but that session's context could NOT be restored — you are starting fresh with no memory of the previous turns. That history is not readable from anywhere now: there is no command that fetches it, and only the context already in this message survives. **When you reply, tell the user up front (one short sentence) that the previous conversation context was unavailable and this is a new session**, so they understand why the thread did not carry over.\n\n"

const AutopilotIssueCommandsGuard = "Do not run `multica issue get`, `multica issue comment add`, or `multica issue status` for this run unless the autopilot instructions explicitly tell you to create or update an issue"
