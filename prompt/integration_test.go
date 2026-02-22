package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFullPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("soul text"), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agent rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("identity profile"), 0o600); err != nil {
		t.Fatalf("write IDENTITY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "USER.md"), []byte("user goals"), 0o600); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("recent context"), 0o600); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("workspace heartbeat"), 0o600); err != nil {
		t.Fatalf("write HEARTBEAT.md: %v", err)
	}

	skill1 := filepath.Join(dir, "skills", "alpha", "SKILL.md")
	skill2 := filepath.Join(dir, "skills", "zeta", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill1), 0o700); err != nil {
		t.Fatalf("mkdir skill1: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(skill2), 0o700); err != nil {
		t.Fatalf("mkdir skill2: %v", err)
	}
	if err := os.WriteFile(skill1, []byte("---\nname: alpha\ndescription: Alpha skill\n---\n"), 0o600); err != nil {
		t.Fatalf("write alpha skill: %v", err)
	}
	if err := os.WriteFile(skill2, []byte("---\nname: zeta\ndescription: Zeta skill\n---\n"), 0o600); err != nil {
		t.Fatalf("write zeta skill: %v", err)
	}

	ws, err := LoadWorkspace(dir)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	got := BuildSystemPrompt(SystemPromptParams{
		Mode:         "full",
		Workspace:    ws,
		Skills:       skills,
		MemoryRecall: "recall line",
		DateTime:     time.Date(2026, 2, 21, 9, 15, 0, 0, time.UTC),
		Heartbeat:    "live heartbeat",
		RuntimeInfo:  "runtime details",
	})

	present := []string{
		"## Identity\n",
		"## Messaging\n",
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
	for _, h := range present {
		if !strings.Contains(got, h) {
			t.Fatalf("expected section %q in prompt: %s", h, got)
		}
	}
	if strings.Contains(got, "## Tooling\n") {
		t.Fatalf("unexpected Tooling section: %s", got)
	}

	order := []string{
		"## Identity\n",
		"## Messaging\n",
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
	for _, h := range order {
		i := strings.Index(got, h)
		if i < 0 {
			t.Fatalf("missing section %q", h)
		}
		if i <= last {
			t.Fatalf("section %q is out of order", h)
		}
		last = i
	}

	for _, h := range []string{
		"### SOUL.md\nsoul text",
		"### AGENTS.md\nagent rules",
		"### IDENTITY.md\nidentity profile",
		"### USER.md\nuser goals",
		"### MEMORY.md\nrecent context",
		"### HEARTBEAT.md\nworkspace heartbeat",
		"- alpha: Alpha skill (skills/alpha/SKILL.md)",
		"- zeta: Zeta skill (skills/zeta/SKILL.md)",
	} {
		if !strings.Contains(got, h) {
			t.Fatalf("expected %q in prompt", h)
		}
	}
}

func TestMinimalPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("soul text"), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agent rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("identity profile"), 0o600); err != nil {
		t.Fatalf("write IDENTITY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "USER.md"), []byte("user goals"), 0o600); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	ws, err := LoadWorkspace(dir)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	got := BuildSystemPrompt(SystemPromptParams{
		Mode:         "minimal",
		Workspace:    ws,
		MemoryRecall: "ignored",
		DateTime:     time.Date(2026, 2, 21, 9, 15, 0, 0, time.UTC),
		Heartbeat:    "minimal heartbeat",
		RuntimeInfo:  "ignored too",
	})

	present := []string{
		"## Identity\n",
		"## Messaging\n",
		"## Workspace\n",
		"## Workspace Files\n",
		"## Heartbeat\n",
	}
	for _, h := range present {
		if !strings.Contains(got, h) {
			t.Fatalf("expected section %q in prompt: %s", h, got)
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
		"### SOUL.md",
		"### USER.md",
		"### IDENTITY.md",
		"### MEMORY.md",
		"### HEARTBEAT.md",
	}
	for _, h := range absent {
		if strings.Contains(got, h) {
			t.Fatalf("expected section %q to be absent: %s", h, got)
		}
	}
	if !strings.Contains(got, "### AGENTS.md\nagent rules") {
		t.Fatalf("expected AGENTS.md in minimal workspace files: %s", got)
	}
}

func TestTruncation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	content := strings.Repeat("A", 15000) + strings.Repeat("B", 8000) + strings.Repeat("C", 7000)
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agent rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	ws, err := LoadWorkspace(dir)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	if len(ws.Soul) != maxWorkspaceFileChars {
		t.Fatalf("expected soul file to be truncated to %d, got %d", maxWorkspaceFileChars, len(ws.Soul))
	}
	if !strings.Contains(ws.Soul, "[truncated ") {
		t.Fatalf("expected truncated marker: %s", ws.Soul)
	}
	if !strings.HasPrefix(ws.Soul, strings.Repeat("A", 20)) {
		t.Fatalf("expected head preserved: %s", ws.Soul)
	}
	if !strings.HasSuffix(ws.Soul, strings.Repeat("C", 20)) {
		t.Fatalf("expected tail preserved: %s", ws.Soul)
	}

	got := BuildSystemPrompt(SystemPromptParams{
		Mode:      "full",
		Workspace: ws,
		Skills:    nil,
	})
	if !strings.Contains(got, "### SOUL.md\n") {
		t.Fatalf("expected SOUL.md in Workspace Files: %s", got)
	}

	gotPrompt := strings.Index(got, ws.Soul[:10])
	if gotPrompt < 0 {
		t.Fatalf("expected truncated soul content in prompt")
	}
}

func TestMissingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("soul text"), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agent rules"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	ws, err := LoadWorkspace(dir)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	got := BuildSystemPrompt(SystemPromptParams{
		Mode:         "full",
		Workspace:    ws,
		MemoryRecall: "",
		Heartbeat:    "external heartbeat",
	})

	present := []string{
		"### SOUL.md\nsoul text",
		"### AGENTS.md\nagent rules",
		"## Identity\n",
		"## Workspace Files\n",
		"## Heartbeat\n",
	}
	for _, h := range present {
		if !strings.Contains(got, h) {
			t.Fatalf("expected %q in prompt: %s", h, got)
		}
	}

	missing := []string{
		"### IDENTITY.md",
		"### USER.md",
		"### MEMORY.md",
		"### HEARTBEAT.md",
		"## Skills\n",
		"## Memory Recall\n",
		"## Date/Time\n",
		"## Runtime\n",
		"## Tooling\n",
		"## Workspace\n",
	}
	for _, h := range missing {
		if strings.Contains(got, h) {
			t.Fatalf("expected %q to be omitted: %s", h, got)
		}
	}
}

func TestEmptyWorkspace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ws, err := LoadWorkspace(dir)
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	got := BuildSystemPrompt(SystemPromptParams{
		Mode:      "minimal",
		Workspace: ws,
		Heartbeat: "heartbeat ok",
	})
	if got == "" {
		t.Fatal("expected minimal prompt for empty workspace")
	}
	if !strings.Contains(got, "## Heartbeat\n") {
		t.Fatalf("expected heartbeat section: %s", got)
	}
	if strings.Contains(got, "## Identity\n") || strings.Contains(got, "## Workspace Files\n") {
		t.Fatalf("did not expect identity/workspace files in empty workspace prompt: %s", got)
	}
}
