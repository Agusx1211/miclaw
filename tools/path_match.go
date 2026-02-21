package tools

import (
	"path"
	"strings"
)

func matchPathPattern(pattern, target string) bool {

	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	t := strings.TrimSuffix(strings.TrimPrefix(target, "./"), "/")
	p = path.Clean(p)
	t = path.Clean(t)

	if !strings.Contains(p, "/") {
		return matchPathSegment(p, path.Base(t))
	}
	return matchPathSegments(strings.Split(p, "/"), strings.Split(t, "/"), 0, 0)
}

func matchPathSegments(patterns, targets []string, pi, ti int) bool {

	if pi == len(patterns) {
		return ti == len(targets)
	}
	s := patterns[pi]
	if s == "**" {
		if pi+1 == len(patterns) {
			return true
		}
		for next := ti; next <= len(targets); next++ {
			if matchPathSegments(patterns, targets, pi+1, next) {
				return true
			}
		}
		return false
	}
	if ti >= len(targets) {
		return false
	}
	if !matchPathSegment(s, targets[ti]) {
		return false
	}
	return matchPathSegments(patterns, targets, pi+1, ti+1)
}

func matchPathSegment(pattern, target string) bool {

	ok, _ := path.Match(pattern, target)

	return ok
}
