package prompt

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var truncatedChars = regexp.MustCompile(`\[truncated ([0-9]+) chars\]`)

func TestTruncateShortContentUnchanged(t *testing.T) {
	t.Parallel()
	in := "short content"
	out := Truncate(in, 20)
	if out != in {
		t.Fatalf("expected unchanged content, got %q", out)
	}
}

func TestTruncateExactlyAtLimit(t *testing.T) {
	t.Parallel()
	in := "exactly 12!"
	out := Truncate(in, len(in))
	if out != in {
		t.Fatalf("expected %q, got %q", in, out)
	}
}

func TestTruncatePreservesHeadAndTail(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("H", 700) + strings.Repeat("M", 500) + strings.Repeat("T", 700)
	out := Truncate(in, 1000)
	if !strings.HasPrefix(out, strings.Repeat("H", 700)) {
		t.Fatal("expected truncated head to be preserved")
	}
	if !strings.HasSuffix(out, strings.Repeat("T", 200)) {
		t.Fatal("expected truncated tail to be preserved")
	}
	if len(out) != 1000 {
		t.Fatalf("expected len 1000, got %d", len(out))
	}
}

func TestTruncateMarkerIncludesCharCount(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("a", 1200)
	out := Truncate(in, 1000)
	m := truncatedChars.FindStringSubmatch(out)
	if len(m) != 2 {
		t.Fatalf("marker missing or malformed: %q", out)
	}
	if m[1] != "200" {
		t.Fatalf("unexpected omitted count: %s", m[1])
	}
	if !strings.Contains(out, fmt.Sprintf("[truncated %d chars]", 200)) {
		t.Fatal("marker should include omitted chars")
	}
}

func TestTruncateEmptyString(t *testing.T) {
	t.Parallel()
	if got := Truncate("", 1); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
