package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxSkillMarkdownBytes = 256 * 1024
	maxSkills             = 150
	maxSkillSummaryBytes  = 30 * 1024
)

type SkillSummary struct {
	Name        string
	Description string
	Path        string
}

func LoadSkills(workspacePath string) ([]SkillSummary, error) {
	if workspacePath == "" {
		panic("workspace path is required")
	}

	paths, err := filepath.Glob(filepath.Join(workspacePath, "skills", "*", "SKILL.md"))
	if err != nil {
		return nil, err
	}

	summaries := make([]SkillSummary, 0, len(paths))
	for _, p := range paths {
		s, ok, err := parseSkillFile(p, workspacePath)
		if err != nil {
			return nil, err
		}
		if ok {
			summaries = append(summaries, s)
		}
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	if len(summaries) > maxSkills {
		summaries = summaries[:maxSkills]
	}

	truncateSummaries(summaries)

	if !sort.SliceIsSorted(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	}) {
		panic("skills not sorted by name")
	}
	return summaries, nil
}

func parseSkillFile(path, base string) (SkillSummary, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return SkillSummary{}, false, fmt.Errorf("stat skill file %s: %w", path, err)
	}
	if info.Size() > maxSkillMarkdownBytes {
		return SkillSummary{}, false, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return SkillSummary{}, false, fmt.Errorf("read skill file %s: %w", path, err)
	}

	metadata, _ := ParseFrontmatter(string(content))
	name := strings.TrimSpace(metadata["name"])
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return SkillSummary{}, false, fmt.Errorf("rel path %s: %w", path, err)
	}

	return SkillSummary{
		Name:        name,
		Description: strings.TrimSpace(metadata["description"]),
		Path:        filepath.ToSlash(rel),
	}, true, nil
}

func truncateSummaries(summaries []SkillSummary) {
	remaining := maxSkillSummaryBytes
	for i, s := range summaries {
		remaining -= len(s.Name)
		if remaining < 0 {
			panic("skill summary name overflow")
		}
		if remaining >= len(s.Description) {
			remaining -= len(s.Description)
			continue
		}
		if remaining <= 0 {
			summaries[i].Description = ""
			continue
		}
		summaries[i].Description = s.Description[:remaining]
		remaining = 0
	}
}
