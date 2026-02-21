package agent

import "testing"

func TestCheckCompaction(t *testing.T) {
	s := &Session{PromptTokens: 12, CompletionTokens: 9}
	if !checkCompaction(s, 20) {
		t.Fatal("expected compaction when total tokens exceed threshold")
	}
	if checkCompaction(s, 21) {
		t.Fatal("did not expect compaction when total tokens equal threshold")
	}
}
