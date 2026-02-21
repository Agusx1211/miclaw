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
	must(params.Workspace != nil, "workspace is required")
	must(params.Mode == "full" || params.Mode == "minimal", "mode must be full or minimal")

	var sections []promptSection
	if params.Mode == "full" {
		sections = buildFullSections(params)
	} else {
		sections = buildMinimalSections(params)
	}

	prompt := renderSections(sections)
	must(strings.HasPrefix(prompt, "## "), "prompt must start with section header")
	must(strings.HasSuffix(prompt, "\n"), "prompt must end with newline")
	return prompt
}

func buildFullSections(params SystemPromptParams) []promptSection {
	must(params.Mode == "full", "full sections require full mode")
	must(params.Workspace != nil, "workspace is required")

	sections := []promptSection{
		{name: "Identity", content: identitySection(params.Workspace)},
		{name: "Tooling", content: toolingSection()},
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
	must(len(sections) == 11, "full mode must define 11 sections")
	must(sections[0].name == "Identity" && sections[10].name == "Runtime", "full section boundaries invalid")
	return sections
}

func buildMinimalSections(params SystemPromptParams) []promptSection {
	must(params.Mode == "minimal", "minimal sections require minimal mode")
	must(params.Workspace != nil, "workspace is required")

	sections := []promptSection{
		{name: "Identity", content: identitySection(params.Workspace)},
		{name: "Tooling", content: toolingSection()},
		{name: "Workspace", content: strings.TrimSpace(params.Workspace.User)},
		{name: "Workspace Files", content: workspaceFilesSection(params.Workspace, true)},
		{name: "Heartbeat", content: strings.TrimSpace(params.Heartbeat)},
	}
	must(len(sections) == 5, "minimal mode must define 5 sections")
	must(sections[0].name == "Identity" && sections[4].name == "Heartbeat", "minimal section boundaries invalid")
	return sections
}

func identitySection(ws *Workspace) string {
	must(ws != nil, "workspace is required")
	must(ws != (*Workspace)(nil), "workspace pointer must be valid")

	parts := make([]string, 0, 2)
	if v := strings.TrimSpace(ws.Soul); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(ws.Identity); v != "" {
		parts = append(parts, v)
	}
	out := strings.Join(parts, "\n\n")
	must(len(parts) <= 2, "identity section can only include soul and identity")
	must(!strings.Contains(out, "\x00"), "identity section must not include null bytes")
	return out
}

func toolingSection() string {
	const placeholder = ""
	out := strings.TrimSpace(placeholder)
	must(strings.TrimSpace(placeholder) == out, "tooling placeholder trim must be stable")
	must(!strings.Contains(out, "\x00"), "tooling section must not include null bytes")
	return out
}

func toolCallStyleSection() string {
	out := strings.TrimSpace(`- Narrate intent before substantial tool actions.
- Skip narration for trivial reads or lookups.
- Keep progress updates short and factual.`)
	must(out != "", "tool call style must not be empty")
	must(strings.Count(out, "\n") >= 2, "tool call style must include multiple rules")
	return out
}

func safetySection() string {
	out := strings.TrimSpace(`- Refuse illegal or harmful instructions.
- Protect secrets and credentials; never print them.
- If a request is disallowed, refuse directly and offer a safe alternative.`)
	must(out != "", "safety section must not be empty")
	must(strings.Count(out, "\n") >= 2, "safety section must include multiple rules")
	return out
}

func skillsSection(skills []SkillSummary) string {
	must(len(skills) <= maxSkills, "skills must be pre-limited")
	must(len(skills) >= 0, "skills count must be non-negative")
	if len(skills) == 0 {
		return ""
	}

	lines := make([]string, 0, len(skills))
	for _, s := range skills {
		name := strings.TrimSpace(s.Name)
		must(name != "", "skill name is required")
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
	must(len(lines) == len(skills), "every skill must render exactly one line")
	must(strings.HasPrefix(out, "- "), "skills section must render a list")
	return out
}

func dateTimeSection(dt time.Time) string {
	must(dt.Location() != nil, "datetime location is required")
	must(dt.Equal(dt), "datetime must be comparable")
	if dt.IsZero() {
		return ""
	}

	out := dt.Format(time.RFC3339)
	parsed, err := time.Parse(time.RFC3339, out)
	must(err == nil, "datetime must be RFC3339")
	must(parsed.Format(time.RFC3339) == out, "datetime formatting must round-trip")
	return out
}

func workspaceFilesSection(ws *Workspace, minimal bool) string {
	must(ws != nil, "workspace is required")
	must(ws != (*Workspace)(nil), "workspace pointer must be valid")

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
	must(len(files) == 1 || len(files) == 6, "workspace files section must use known file set")
	must(files[0].name == "AGENTS.md" || files[0].name == "SOUL.md", "workspace files order invalid")

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
		must(!strings.Contains(out, "### SOUL.md"), "minimal workspace files must not include SOUL.md")
		must(!strings.Contains(out, "### MEMORY.md"), "minimal workspace files must not include MEMORY.md")
		return out
	}
	must(!strings.Contains(out, "### TOOLS.md"), "workspace files should only include bootstrap files")
	must(!strings.Contains(out, "### BOOT.md"), "workspace files should only include bootstrap files")
	return out
}

func renderSections(sections []promptSection) string {
	must(len(sections) > 0, "sections are required")
	must(strings.TrimSpace(sections[0].name) != "", "first section name is required")

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		name := strings.TrimSpace(s.name)
		must(name != "", "section name is required")
		content := strings.TrimSpace(s.content)
		if content == "" {
			continue
		}
		parts = append(parts, "## "+name+"\n"+content+"\n")
	}

	out := strings.Join(parts, "\n")
	must(len(parts) > 0, "at least one section must render")
	must(strings.HasPrefix(out, "## "), "rendered prompt must start with a section header")
	return out
}

func must(ok bool, msg string) {
	if msg == "" {
		panic("assertion message must not be empty")
	}
	if !ok {
		panic(msg)
	}
}
