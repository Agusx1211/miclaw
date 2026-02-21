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
