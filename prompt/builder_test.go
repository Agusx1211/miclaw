package prompt

import (
	"strings"
	"testing"
	"time"
)

func TestBuildSystemPromptFullAllSectionsPopulated(t *testing.T) {
	t.Parallel()
	ws := &Workspace{
		Soul:      "soul line",
		Agents:    "agents line",
		Identity:  "identity line",
		User:      "user prefs",
		Memory:    "memory body",
		Heartbeat: "workspace heartbeat",
	}
	params := SystemPromptParams{
		Mode:      "full",
		Workspace: ws,
		Skills: []SkillSummary{
			{Name: "alpha", Description: "Alpha skill", Path: "skills/alpha/SKILL.md"},
			{Name: "beta", Description: "Beta skill", Path: "skills/beta/SKILL.md"},
		},
		MemoryRecall: "Recent memory snippets",
		DateTime:     time.Date(2026, 2, 21, 5, 21, 0, 0, time.UTC),
		Heartbeat:    "Reply HEARTBEAT_OK to health checks",
		RuntimeInfo:  "go1.24 linux amd64",
	}

	got := BuildSystemPrompt(params)

	headers := []string{
		"## Identity\n",
		"## Tool Call Style\n",
		"## Safety\n",
		"## Skills\n",
		"## Memory Recall\n",
		"## Workspace\n",
		"## Date/Time\n",
		"## Workspace Files\n",
		"## Heartbeat\n",
		"## Runtime\n",
	}
	for _, h := range headers {
		if !strings.Contains(got, h) {
			t.Fatalf("expected header %q, got prompt:\n%s", h, got)
		}
	}

	if strings.Contains(got, "## Tooling\n") {
		t.Fatalf("expected tooling section to be skipped when placeholder is empty, got:\n%s", got)
	}
}

func TestBuildSystemPromptFullSkipsEmptySections(t *testing.T) {
	t.Parallel()
	ws := &Workspace{Soul: "s", Identity: "i", Agents: "a"}
	params := SystemPromptParams{Mode: "full", Workspace: ws}

	got := BuildSystemPrompt(params)

	if !strings.Contains(got, "## Identity\n") {
		t.Fatalf("expected identity section, got:\n%s", got)
	}
	if !strings.Contains(got, "## Tool Call Style\n") {
		t.Fatalf("expected tool call style section, got:\n%s", got)
	}
	if !strings.Contains(got, "## Safety\n") {
		t.Fatalf("expected safety section, got:\n%s", got)
	}
	if !strings.Contains(got, "## Workspace Files\n") {
		t.Fatalf("expected workspace files section, got:\n%s", got)
	}

	skipped := []string{
		"## Tooling\n",
		"## Skills\n",
		"## Memory Recall\n",
		"## Workspace\n",
		"## Date/Time\n",
		"## Heartbeat\n",
		"## Runtime\n",
	}
	for _, h := range skipped {
		if strings.Contains(got, h) {
			t.Fatalf("expected header %q to be skipped, got:\n%s", h, got)
		}
	}
}

func TestBuildSystemPromptMinimalIncludesCorrectSections(t *testing.T) {
	t.Parallel()
	ws := &Workspace{
		Soul:      "soul",
		Agents:    "agents",
		Identity:  "identity",
		User:      "user",
		Memory:    "memory",
		Heartbeat: "workspace-heartbeat",
	}
	params := SystemPromptParams{
		Mode:         "minimal",
		Workspace:    ws,
		Skills:       []SkillSummary{{Name: "alpha", Description: "x", Path: "skills/alpha/SKILL.md"}},
		MemoryRecall: "should not show",
		DateTime:     time.Date(2026, 2, 21, 5, 21, 0, 0, time.UTC),
		Heartbeat:    "hb",
		RuntimeInfo:  "runtime",
	}

	got := BuildSystemPrompt(params)

	present := []string{
		"## Identity\n",
		"## Workspace\n",
		"## Workspace Files\n",
		"## Heartbeat\n",
	}
	for _, h := range present {
		if !strings.Contains(got, h) {
			t.Fatalf("expected header %q, got:\n%s", h, got)
		}
	}

	absent := []string{
		"## Tooling\n",
		"## Tool Call Style\n",
		"## Safety\n",
		"## Skills\n",
		"## Memory Recall\n",
		"## Date/Time\n",
		"## Runtime\n",
	}
	for _, h := range absent {
		if strings.Contains(got, h) {
			t.Fatalf("expected header %q to be absent, got:\n%s", h, got)
		}
	}
}

func TestBuildSystemPromptMinimalWorkspaceFilesOnlyAgents(t *testing.T) {
	t.Parallel()
	ws := &Workspace{
		Soul:      "soul-only",
		Agents:    "agents-only",
		Identity:  "identity-only",
		User:      "user-only",
		Memory:    "memory-only",
		Heartbeat: "hb-only",
	}
	got := BuildSystemPrompt(SystemPromptParams{Mode: "minimal", Workspace: ws, Heartbeat: "heartbeat"})

	if !strings.Contains(got, "### AGENTS.md\nagents-only") {
		t.Fatalf("expected AGENTS.md in workspace files, got:\n%s", got)
	}
	for _, disallowed := range []string{"### SOUL.md", "### IDENTITY.md", "### USER.md", "### MEMORY.md", "### HEARTBEAT.md"} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("did not expect %q in minimal workspace files, got:\n%s", disallowed, got)
		}
	}
}

func TestBuildSystemPromptDateTimeFormattedCorrectly(t *testing.T) {
	t.Parallel()
	dt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC-5", -5*3600))
	got := BuildSystemPrompt(SystemPromptParams{
		Mode:      "full",
		Workspace: &Workspace{Soul: "s", Identity: "i", Agents: "a"},
		DateTime:  dt,
	})

	if !strings.Contains(got, "## Date/Time\n") {
		t.Fatalf("expected date/time section, got:\n%s", got)
	}
	if !strings.Contains(got, "2026-01-02T03:04:05-05:00") {
		t.Fatalf("expected RFC3339 datetime, got:\n%s", got)
	}
}

func TestBuildSystemPromptSectionOrdering(t *testing.T) {
	t.Parallel()
	got := BuildSystemPrompt(SystemPromptParams{
		Mode: "full",
		Workspace: &Workspace{
			Soul:      "soul",
			Agents:    "agents",
			Identity:  "identity",
			User:      "user",
			Memory:    "memory",
			Heartbeat: "workspace-hb",
		},
		Skills:       []SkillSummary{{Name: "alpha", Description: "desc", Path: "skills/alpha/SKILL.md"}},
		MemoryRecall: "mem",
		DateTime:     time.Date(2026, 2, 21, 5, 21, 0, 0, time.UTC),
		Heartbeat:    "heartbeat",
		RuntimeInfo:  "runtime",
	})

	orderedHeaders := []string{
		"## Identity\n",
		"## Tool Call Style\n",
		"## Safety\n",
		"## Skills\n",
		"## Memory Recall\n",
		"## Workspace\n",
		"## Date/Time\n",
		"## Workspace Files\n",
		"## Heartbeat\n",
		"## Runtime\n",
	}

	last := -1
	for _, h := range orderedHeaders {
		i := strings.Index(got, h)
		if i < 0 {
			t.Fatalf("missing header %q in prompt:\n%s", h, got)
		}
		if i <= last {
			t.Fatalf("header %q out of order in prompt:\n%s", h, got)
		}
		last = i
	}
}
