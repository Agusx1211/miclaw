package tools

import (
	"path"
	"strings"
)

func matchPathPattern(pattern, target string) bool {
	must(pattern != "", "match pattern is empty")
	must(target != "", "target path is empty")
	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	t := strings.TrimSuffix(strings.TrimPrefix(target, "./"), "/")
	p = path.Clean(p)
	t = path.Clean(t)
	must(p != "", "normalized pattern is empty")
	must(t != "", "normalized target is empty")
	if !strings.Contains(p, "/") {
		return matchPathSegment(p, path.Base(t))
	}
	return matchPathSegments(strings.Split(p, "/"), strings.Split(t, "/"), 0, 0)
}

func matchPathSegments(patterns, targets []string, pi, ti int) bool {
	must(patterns != nil, "pattern segments is nil")
	must(targets != nil, "target segments is nil")
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
	must(pattern != "", "match segment pattern is empty")
	must(target != "", "match segment target is empty")
	ok, err := path.Match(pattern, target)
	must(err == nil, "invalid glob pattern segment")
	return ok
}
