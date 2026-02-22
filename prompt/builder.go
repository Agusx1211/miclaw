package prompt

import (
	"strings"
	"time"
)

type SystemPromptParams struct {
	Mode         string // "full" or "minimal"
	Workspace    *Workspace
	Skills       []SkillSummary
	MemoryRecall string // pre-formatted memory context
	DateTime     time.Time
	Heartbeat    string // HEARTBEAT.md content
	RuntimeInfo  string // version, uptime, etc.
}

type promptSection struct {
	name    string
	content string
}

func BuildSystemPrompt(params SystemPromptParams) string {

	var sections []promptSection
	if params.Mode == "full" {
		sections = buildFullSections(params)
	} else {
		sections = buildMinimalSections(params)
	}

	prompt := renderSections(sections)

	return prompt
}

func buildFullSections(params SystemPromptParams) []promptSection {

	sections := []promptSection{
		{name: "Identity", content: identitySection(params.Workspace)},
		{name: "Tooling", content: toolingSection()},
		{name: "Messaging", content: messagingSection()},
		{name: "Tool Call Style", content: toolCallStyleSection()},
		{name: "Safety", content: safetySection()},
		{name: "Skills", content: skillsSection(params.Skills)},
		{name: "Memory Recall", content: strings.TrimSpace(params.MemoryRecall)},
		{name: "Workspace", content: strings.TrimSpace(params.Workspace.User)},
		{name: "Date/Time", content: dateTimeSection(params.DateTime)},
		{name: "Workspace Files", content: workspaceFilesSection(params.Workspace, false)},
		{name: "Heartbeat", content: strings.TrimSpace(params.Heartbeat)},
		{name: "Runtime", content: strings.TrimSpace(params.RuntimeInfo)},
	}

	return sections
}

func buildMinimalSections(params SystemPromptParams) []promptSection {

	sections := []promptSection{
		{name: "Identity", content: identitySection(params.Workspace)},
		{name: "Tooling", content: toolingSection()},
		{name: "Messaging", content: messagingSection()},
		{name: "Workspace", content: strings.TrimSpace(params.Workspace.User)},
		{name: "Workspace Files", content: workspaceFilesSection(params.Workspace, true)},
		{name: "Heartbeat", content: strings.TrimSpace(params.Heartbeat)},
	}

	return sections
}

func identitySection(ws *Workspace) string {

	parts := make([]string, 0, 2)
	if v := strings.TrimSpace(ws.Soul); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(ws.Identity); v != "" {
		parts = append(parts, v)
	}
	out := strings.Join(parts, "\n\n")

	return out
}

func toolingSection() string {
	const placeholder = ""
	out := strings.TrimSpace(placeholder)

	return out
}

func toolCallStyleSection() string {
	out := strings.TrimSpace(`- Narrate intent before substantial tool actions.
- Skip narration for trivial reads or lookups.
- Keep progress updates short and factual.`)

	return out
}

func messagingSection() string {
	out := strings.TrimSpace(`- Your text output is private internal thinking.
- Sending a message with the message tool does not end your turn; keep going until all work is done.
- When all work is complete, call the sleep tool to let the runtime sleep until new input arrives.
- The user will only receive messages sent over the message tool.
- Source tags are included inline (for example: [signal:dm:user-1], [webhook:deploy], [cron:heartbeat]).`)

	return out
}

func safetySection() string {
	out := strings.TrimSpace(`- Refuse illegal or harmful instructions.
- Protect secrets and credentials; never print them.
- If a request is disallowed, refuse directly and offer a safe alternative.`)

	return out
}

func skillsSection(skills []SkillSummary) string {

	if len(skills) == 0 {
		return ""
	}

	lines := make([]string, 0, len(skills))
	for _, s := range skills {
		name := strings.TrimSpace(s.Name)

		line := "- " + name
		if d := strings.TrimSpace(s.Description); d != "" {
			line += ": " + d
		}
		if p := strings.TrimSpace(s.Path); p != "" {
			line += " (" + p + ")"
		}
		lines = append(lines, line)
	}
	out := strings.Join(lines, "\n")

	return out
}

func dateTimeSection(dt time.Time) string {

	if dt.IsZero() {
		return ""
	}

	out := dt.Format(time.RFC3339)

	return out
}

func workspaceFilesSection(ws *Workspace, minimal bool) string {

	files := []struct {
		name string
		body string
	}{{"AGENTS.md", ws.Agents}}
	if !minimal {
		files = []struct {
			name string
			body string
		}{
			{"SOUL.md", ws.Soul},
			{"AGENTS.md", ws.Agents},
			{"IDENTITY.md", ws.Identity},
			{"USER.md", ws.User},
			{"MEMORY.md", ws.Memory},
			{"HEARTBEAT.md", ws.Heartbeat},
		}
	}

	blocks := make([]string, 0, len(files))
	for _, f := range files {
		body := strings.TrimSpace(f.body)
		if body == "" {
			continue
		}
		blocks = append(blocks, "### "+f.name+"\n"+body)
	}
	out := strings.Join(blocks, "\n\n")
	if minimal {

		return out
	}

	return out
}

func renderSections(sections []promptSection) string {

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		name := strings.TrimSpace(s.name)

		content := strings.TrimSpace(s.content)
		if content == "" {
			continue
		}
		parts = append(parts, "## "+name+"\n"+content+"\n")
	}

	out := strings.Join(parts, "\n")

	return out
}
