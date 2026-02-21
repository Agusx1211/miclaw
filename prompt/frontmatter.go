package prompt

import "strings"

func ParseFrontmatter(content string) (map[string]string, string) {
	frontmatter := map[string]string{}
	if !strings.HasPrefix(content, "---\n") {
		return frontmatter, content
	}

	close := strings.Index(content[4:], "---\n")
	if close < 0 {
		return frontmatter, content
	}
	close += 4

	for _, line := range strings.Split(content[4:close], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sep := strings.Index(line, ":")
		if sep < 0 {
			continue
		}
		key := strings.TrimSpace(line[:sep])
		value := strings.TrimSpace(line[sep+1:])
		frontmatter[key] = value
	}

	return frontmatter, content[close+4:]
}
