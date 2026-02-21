package prompt

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxWorkspaceFileChars  = 20000
	maxWorkspaceTotalChars = 150000
)

type Workspace struct {
	Soul      string
	Agents    string
	Identity  string
	User      string
	Memory    string
	Heartbeat string
}

func LoadWorkspace(path string) (*Workspace, error) {
	if path == "" {
		panic("workspace path is required")
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	w := &Workspace{}
	files := []struct {
		name string
		dst  *string
	}{
		{"SOUL.md", &w.Soul},
		{"AGENTS.md", &w.Agents},
		{"IDENTITY.md", &w.Identity},
		{"USER.md", &w.User},
		{"MEMORY.md", &w.Memory},
		{"HEARTBEAT.md", &w.Heartbeat},
	}

	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(path, file.name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", file.name, err)
		}
		*file.dst = string(content)
	}

	truncateWorkspaceToLimit(files, maxWorkspaceTotalChars)

	for i := range files {
		if len(*files[i].dst) <= maxWorkspaceFileChars {
			continue
		}
		*files[i].dst = Truncate(*files[i].dst, maxWorkspaceFileChars)
	}
	if len(w.Soul)+len(w.Agents)+len(w.Identity)+len(w.User)+len(w.Memory)+len(w.Heartbeat) > maxWorkspaceTotalChars {
		panic("workspace total exceeds limit")
	}
	return w, nil
}

func truncateWorkspaceToLimit(files []struct {
	name string
	dst  *string
}, limit int) {
	total := 0
	for _, file := range files {
		total += len(*file.dst)
	}
	for total > limit {
		largest := 0
		for i := 1; i < len(files); i++ {
			if len(*files[i].dst) > len(*files[largest].dst) {
				largest = i
			}
		}

		current := len(*files[largest].dst)
		if current == 0 {
			break
		}
		target := current * 9 / 10
		if target == 0 {
			*files[largest].dst = ""
		} else {
			*files[largest].dst = Truncate(*files[largest].dst, target)
		}
		next := 0
		for _, file := range files {
			next += len(*file.dst)
		}
		if next >= total {
			panic("workspace total truncation stalled")
		}
		total = next
	}
}
