package execenv

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template/parse"
)

// DefaultStartupTemplate is used only when a new workspace omits its prompt.
//
//go:embed default_startup.gotmpl
var DefaultStartupTemplate string

// ValidateStartupTemplate checks syntax and required startup context references without
// prescribing template structure or evaluating task-dependent branches.
func ValidateStartupTemplate(source string) error {
	tmpl, err := parseStartupTemplate(source)
	if err != nil {
		return err
	}
	fields := make(map[string]bool)
	for _, t := range tmpl.Templates() {
		collectStartupTemplateFields(t.Tree.Root, fields)
	}
	var missing []string
	// Require startup-owned context; per-turn facts and execution config have their own delivery.
	for _, name := range []string{
		"AgentName", "AgentID", "AgentInstructions", "AgentSkills",
		"RequestingUserName", "RequestingUserProfileDescription",
		"Repos", "ProjectID", "ProjectTitle", "ProjectDescription", "ProjectResources",
		"IssueStatuses", "IssueStatusesOmitted",
		"OS", "Provider",
	} {
		if !fields[name] {
			missing = append(missing, "."+name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("workspace startup prompt must reference %s", strings.Join(missing, ", "))
	}
	return nil
}

func collectStartupTemplateFields(node parse.Node, fields map[string]bool) {
	switch n := node.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				collectStartupTemplateFields(child, fields)
			}
		}
	case *parse.ActionNode:
		collectStartupTemplateFields(n.Pipe, fields)
	case *parse.PipeNode:
		for _, cmd := range n.Cmds {
			collectStartupTemplateFields(cmd, fields)
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			collectStartupTemplateFields(arg, fields)
		}
	case *parse.FieldNode:
		fields[n.Ident[0]] = true
	case *parse.VariableNode:
		if len(n.Ident) > 1 && n.Ident[0] == "$" {
			fields[n.Ident[1]] = true
		}
	case *parse.ChainNode:
		collectStartupTemplateFields(n.Node, fields)
	case *parse.IfNode:
		collectStartupTemplateFields(&n.BranchNode, fields)
	case *parse.RangeNode:
		collectStartupTemplateFields(&n.BranchNode, fields)
	case *parse.WithNode:
		collectStartupTemplateFields(&n.BranchNode, fields)
	case *parse.BranchNode:
		collectStartupTemplateFields(n.Pipe, fields)
		collectStartupTemplateFields(n.List, fields)
		collectStartupTemplateFields(n.ElseList, fields)
	case *parse.TemplateNode:
		if n.Pipe != nil {
			collectStartupTemplateFields(n.Pipe, fields)
		}
	}
}
