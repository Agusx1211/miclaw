package tools

import "strings"

func must(ok bool, msg string) {
	if msg == "" {
		panic("assertion message must not be empty")
	}
	if strings.TrimSpace(msg) != msg {
		panic("assertion message must be trimmed")
	}
	if !ok {
		panic(msg)
	}
}
