package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("create skill dir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write skill file %s: %v", p, err)
	}
}

func TestLoadSkillsValidDirectory(t *testing.T) {
	t.Parallel()
	d := t.TempDir()

	writeSkillFile(t, d, "alpha", "---\nname: alpha\ndescription: Alpha summary\n---\nbody\n")
	writeSkillFile(t, d, "beta", "---\ndescription: Uses fallback name\n---\nbody\n")
	writeSkillFile(t, d, "gamma", "---\nname: gamma\ndescription: Third summary\n---\nbody\n")

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(got))
	}

	if got[0].Name != "alpha" || got[1].Name != "beta" || got[2].Name != "gamma" {
		t.Fatalf("expected sorted names [alpha beta gamma], got %+v", got)
	}
	if got[0].Description != "Alpha summary" || got[1].Name != "beta" || got[1].Description != "Uses fallback name" {
		t.Fatalf("unexpected skill entries: %+v", got)
	}
	if got[0].Path != "skills/alpha/SKILL.md" || got[1].Path != "skills/beta/SKILL.md" || got[2].Path != "skills/gamma/SKILL.md" {
		t.Fatalf("unexpected paths: %+v", got)
	}
}

func TestLoadSkillsEmptyDirectory(t *testing.T) {
	t.Parallel()
	d := t.TempDir()

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no skills, got %d", len(got))
	}
}

func TestLoadSkillsNoSKILLMD(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "skills", "ignored"), 0o700); err != nil {
		t.Fatalf("create ignored dir: %v", err)
	}
	writeSkillFile(t, d, "present", "---\nname: present\ndescription: real\n---\nbody\n")

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if got[0].Name != "present" {
		t.Fatalf("unexpected summary: %+v", got[0])
	}
}

func TestLoadSkillsMax150Limit(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	for i := range 160 {
		writeSkillFile(t, d, fmt.Sprintf("skill-%03d", i), "---\ndescription: x\n---\nbody\n")
	}

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(got) != 150 {
		t.Fatalf("expected 150 skills, got %d", len(got))
	}
	if got[0].Name != "skill-000" || got[149].Name != "skill-149" {
		t.Fatalf("unexpected names at boundaries: %s, %s", got[0].Name, got[149].Name)
	}
}

func TestLoadSkillsOversizedSkipped(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	writeSkillFile(t, d, "small", "---\nname: small\ndescription: works\n---\nbody")
	writeSkillFile(t, d, "big", strings.Repeat("x", 256*1024+1))

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if got[0].Name != "small" {
		t.Fatalf("expected small to remain, got %q", got[0].Name)
	}
}

func TestLoadSkillsSortedByName(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	writeSkillFile(t, d, "zeta", "---\nname: zeta\ndescription: z\n---\n")
	writeSkillFile(t, d, "alpha", "---\nname: alpha\ndescription: a\n---\n")
	writeSkillFile(t, d, "middle", "---\nname: middle\ndescription: m\n---\n")

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if got[0].Name != "alpha" || got[1].Name != "middle" || got[2].Name != "zeta" {
		t.Fatalf("expected sorted order alpha, middle, zeta; got %+v", got)
	}
}

func TestLoadSkillsSummaryLimit(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	writeSkillFile(t, d, "tiny", "---\nname: tiny\ndescription: "+strings.Repeat("a", 20000)+"\n---\n")
	writeSkillFile(t, d, "mini", "---\nname: mini\ndescription: "+strings.Repeat("b", 20000)+"\n---\n")

	got, err := LoadSkills(d)
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	var total int
	for _, g := range got {
		total += len(g.Name) + len(g.Description)
	}
	if total > 30*1024 {
		t.Fatalf("expected total summaries <=30KB, got %d", total)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(got))
	}
	if len(got[1].Description) >= 20000 {
		t.Fatalf("expected second skill truncated, got len=%d", len(got[1].Description))
	}
}
