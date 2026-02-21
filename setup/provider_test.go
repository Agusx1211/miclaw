package setup

import "testing"

func TestFilterModelsByAllTerms(t *testing.T) {
	models := []string{
		"openai/gpt-5.2",
		"openai/gpt-5.2-codex",
		"anthropic/claude-sonnet-4-5",
	}
	got := filterModels(models, "openai codex")
	if len(got) != 1 || got[0] != "openai/gpt-5.2-codex" {
		t.Fatalf("unexpected filtered result: %#v", got)
	}
}

func TestChooseNetworkModeCustom(t *testing.T) {
	if got := chooseNetworkMode("corp-net"); got != "custom" {
		t.Fatalf("mode = %q", got)
	}
}

func TestPickBaseURLDoesNotReuseOtherBackendDefault(t *testing.T) {
	got := pickBaseURL("codex", "openrouter", "https://openrouter.ai/api/v1")
	if got != "https://api.openai.com/v1" {
		t.Fatalf("base url = %q", got)
	}
}

func TestPickBaseURLKeepsCurrentWhenBackendUnchanged(t *testing.T) {
	got := pickBaseURL("codex", "codex", "https://proxy.internal/v1")
	if got != "https://proxy.internal/v1" {
		t.Fatalf("base url = %q", got)
	}
}
