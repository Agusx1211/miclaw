package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

type patchParams struct {
	Path  string
	Patch string
}

type patchHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []patchLine
}

type patchLine struct {
	kind byte
	text string
}

func patchTool() Tool {
	params := JSONSchema{
		Type: "object",
		Properties: map[string]JSONSchema{
			"path": {
				Type: "string",
				Desc: "Path to an existing file",
			},
			"patch": {
				Type: "string",
				Desc: "Unified diff patch text",
			},
		},
		Required: []string{"path", "patch"},
	}

	return tool{
		name:   "apply_patch",
		desc:   "Apply a unified diff patch to an existing file",
		params: params,
		runFn:  runPatch,
	}
}

func runPatch(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

	args, err := parsePatchParams(call.Parameters)
	if err != nil {
		return ToolResult{}, err
	}
	b, err := os.ReadFile(args.Path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("read file %q: %v", args.Path, err)
	}
	lines, trailingNewline := splitFileLines(string(b))
	hunks, err := parseUnifiedPatch(args.Patch)
	if err != nil {
		return ToolResult{}, err
	}
	after, summaryLines, err := applyHunks(lines, hunks)
	if err != nil {
		return ToolResult{}, err
	}
	out := joinFileLines(after, trailingNewline)
	if err := os.WriteFile(args.Path, []byte(out), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("write file %q: %v", args.Path, err)
	}
	msg := fmt.Sprintf("applied %d hunk(s) to %s\n%s", len(hunks), args.Path, strings.Join(summaryLines, "\n"))

	return ToolResult{Content: msg}, nil
}

func parsePatchParams(raw json.RawMessage) (patchParams, error) {

	var input struct {
		Path  *string `json:"path"`
		Patch *string `json:"patch"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return patchParams{}, fmt.Errorf("parse apply_patch parameters: %v", err)
	}
	if input.Path == nil || *input.Path == "" {
		return patchParams{}, errors.New("apply_patch parameter path is required")
	}
	if input.Patch == nil || *input.Patch == "" {
		return patchParams{}, errors.New("apply_patch parameter patch is required")
	}
	out := patchParams{Path: *input.Path, Patch: *input.Patch}

	return out, nil
}

func parseUnifiedPatch(raw string) ([]patchHunk, error) {

	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	hunks := make([]patchHunk, 0, 4)
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "@@ ") {
			continue
		}
		h, next, err := parseOneHunk(lines, i)
		if err != nil {
			return nil, err
		}
		hunks = append(hunks, h)
		i = next - 1
	}
	if len(hunks) == 0 {
		return nil, errors.New("invalid unified diff: no hunks found")
	}

	return hunks, nil
}

func parseOneHunk(lines []string, start int) (patchHunk, int, error) {

	h, err := parseHunkHeader(lines[start])
	if err != nil {
		return patchHunk{}, 0, err
	}
	i := start + 1
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
		line := lines[i]
		if line == `\ No newline at end of file` {
			i++
			continue
		}
		p, err := parsePatchLine(line)
		if err != nil {
			return patchHunk{}, 0, fmt.Errorf("parse hunk line %d: %v", i+1, err)
		}
		h.lines = append(h.lines, p)
		i++
	}
	if len(h.lines) == 0 {
		return patchHunk{}, 0, fmt.Errorf("hunk at line %d has no body", start+1)
	}
	oldSide, newSide := countHunkSides(h.lines)
	if oldSide != h.oldCount || newSide != h.newCount {
		return patchHunk{}, 0, fmt.Errorf(
			"hunk count mismatch at line %d: header -%d +%d, body -%d +%d",
			start+1, h.oldCount, h.newCount, oldSide, newSide,
		)
	}

	return h, i, nil
}

func parseHunkHeader(line string) (patchHunk, error) {

	re := regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return patchHunk{}, fmt.Errorf("invalid hunk header: %q", line)
	}
	oldStart, err := strconv.Atoi(m[1])
	if err != nil {
		return patchHunk{}, fmt.Errorf("invalid old start %q: %v", m[1], err)
	}
	newStart, err := strconv.Atoi(m[3])
	if err != nil {
		return patchHunk{}, fmt.Errorf("invalid new start %q: %v", m[3], err)
	}
	oldCount := 1
	if m[2] != "" {
		oldCount, err = strconv.Atoi(m[2])
		if err != nil {
			return patchHunk{}, fmt.Errorf("invalid old count %q: %v", m[2], err)
		}
	}
	newCount := 1
	if m[4] != "" {
		newCount, err = strconv.Atoi(m[4])
		if err != nil {
			return patchHunk{}, fmt.Errorf("invalid new count %q: %v", m[4], err)
		}
	}
	if oldStart <= 0 || newStart <= 0 || oldCount < 0 || newCount < 0 {
		return patchHunk{}, fmt.Errorf("invalid hunk header values: %q", line)
	}
	h := patchHunk{oldStart: oldStart, oldCount: oldCount, newStart: newStart, newCount: newCount}

	return h, nil
}

func parsePatchLine(line string) (patchLine, error) {

	kind := line[0]
	if kind != ' ' && kind != '+' && kind != '-' {
		return patchLine{}, fmt.Errorf("invalid patch line prefix %q", kind)
	}
	out := patchLine{kind: kind, text: line[1:]}

	return out, nil
}

func countHunkSides(lines []patchLine) (int, int) {

	oldCount, newCount := 0, 0
	for _, line := range lines {
		switch line.kind {
		case ' ':
			oldCount++
			newCount++
		case '-':
			oldCount++
		case '+':
			newCount++
		default:
			panic(fmt.Sprintf("unknown patch line kind %q", line.kind))
		}
	}

	return oldCount, newCount
}

func splitFileLines(s string) ([]string, bool) {

	if s == "" {
		return []string{}, false
	}
	trailingNewline := strings.HasSuffix(s, "\n")
	parts := strings.Split(s, "\n")
	if trailingNewline {
		parts = parts[:len(parts)-1]
	}

	return parts, trailingNewline
}

func joinFileLines(lines []string, trailingNewline bool) string {

	if len(lines) == 0 {
		if trailingNewline {
			return "\n"
		}
		return ""
	}
	out := strings.Join(lines, "\n")
	if trailingNewline {
		out += "\n"
	}

	return out
}

func applyHunks(lines []string, hunks []patchHunk) ([]string, []string, error) {

	out := append([]string{}, lines...)
	summary := make([]string, 0, len(hunks))
	delta := 0
	for i, h := range hunks {
		expected := h.oldStart - 1 + delta
		next, start, nextDelta, err := applySingleHunk(out, h, expected)
		if err != nil {
			return nil, nil, fmt.Errorf("hunk %d failed: %v", i+1, err)
		}
		out = next
		delta += nextDelta
		summary = append(summary, fmt.Sprintf("hunk %d applied at line %d", i+1, start+1))
	}

	return out, summary, nil
}

func applySingleHunk(lines []string, h patchHunk, expected int) ([]string, int, int, error) {

	start, err := findHunkStart(lines, h, expected)
	if err != nil {
		return nil, 0, 0, err
	}
	out, oldSide, newSide, err := rewriteWithHunk(lines, h, start)
	if err != nil {
		return nil, 0, 0, err
	}
	if oldSide != h.oldCount || newSide != h.newCount {
		return nil, 0, 0, fmt.Errorf("hunk body count mismatch: got -%d +%d", oldSide, newSide)
	}
	delta := newSide - oldSide

	return out, start, delta, nil
}

func findHunkStart(lines []string, h patchHunk, expected int) (int, error) {

	target := expected
	if target < 0 {
		target = 0
	}
	if target > len(lines) {
		target = len(lines)
	}
	for _, start := range hunkCandidates(target, len(lines), 3) {
		if hunkMatches(lines, h, start) {
			return start, nil
		}
	}
	return 0, fmt.Errorf("context did not match around expected line %d", expected+1)
}

func hunkCandidates(expected, max, window int) []int {

	seen := map[int]struct{}{}
	out := make([]int, 0, 1+window*2)
	for d := 0; d <= window; d++ {
		for _, c := range []int{expected + d, expected - d} {
			if c < 0 || c > max {
				continue
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
	}

	return out
}

func hunkMatches(lines []string, h patchHunk, start int) bool {

	i := start
	for _, line := range h.lines {
		if line.kind == '+' {
			continue
		}
		if i >= len(lines) || lines[i] != line.text {
			return false
		}
		i++
	}

	return true
}

func rewriteWithHunk(lines []string, h patchHunk, start int) ([]string, int, int, error) {

	out := make([]string, 0, len(lines)+h.newCount-h.oldCount)
	out = append(out, lines[:start]...)
	i, oldSide, newSide := start, 0, 0
	for _, line := range h.lines {
		if line.kind == '+' {
			out = append(out, line.text)
			newSide++
			continue
		}
		if i >= len(lines) || lines[i] != line.text {
			return nil, 0, 0, fmt.Errorf("context mismatch at line %d", i+1)
		}
		if line.kind == ' ' {
			out = append(out, line.text)
			newSide++
		}
		oldSide++
		i++
	}
	out = append(out, lines[i:]...)

	return out, oldSide, newSide, nil
}
