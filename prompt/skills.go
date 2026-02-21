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

	pattern := filepath.Join(workspacePath, "skills", "*", "SKILL.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return []SkillSummary{}, nil
	}

	summaries := make([]SkillSummary, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("stat skill file %s: %w", p, err)
		}
		if info.Size() > maxSkillMarkdownBytes {
			continue
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read skill file %s: %w", p, err)
		}

		metadata, _ := ParseFrontmatter(string(content))
		name := strings.TrimSpace(metadata["name"])
		if name == "" {
			name = filepath.Base(filepath.Dir(p))
		}

		rel, err := filepath.Rel(workspacePath, p)
		if err != nil {
			return nil, fmt.Errorf("rel path %s: %w", p, err)
		}

		summaries = append(summaries, SkillSummary{
			Name:        name,
			Description: strings.TrimSpace(metadata["description"]),
			Path:        filepath.ToSlash(rel),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	if len(summaries) > maxSkills {
		summaries = summaries[:maxSkills]
	}

	remaining := maxSkillSummaryBytes
	for i, summary := range summaries {
		remaining -= len(summary.Name)
		if remaining < 0 {
			panic("skill summary name overflow")
		}

		if remaining >= len(summary.Description) {
			remaining -= len(summary.Description)
			continue
		}

		if remaining <= 0 {
			summaries[i].Description = ""
			continue
		}
		summaries[i].Description = summary.Description[:remaining]
		remaining = 0
	}

	if !sort.SliceIsSorted(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	}) {
		panic("skills not sorted by name")
	}

	return summaries, nil
}
