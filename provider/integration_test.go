//go:build integration

package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

func loadAPIKey(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
	}
	devVarsPath := filepath.Join(wd, "..", "DEV_VARS.md")
	f, err := os.Open(devVarsPath)
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		trimmed := strings.TrimPrefix(line, "export ")
		if strings.HasPrefix(trimmed, "OPENROUTER_API_KEY=") {
			key := strings.TrimPrefix(trimmed, "OPENROUTER_API_KEY=")
			key = strings.Trim(key, "\"'")
			if key == "" {
				t.Skip("DEV_VARS.md not found or empty")
			}
			return key
		}
	}
	t.Skip("DEV_VARS.md not found or empty")
	return ""
}

func integrationProvider(t *testing.T) *OpenRouter {
	t.Helper()
	key := loadAPIKey(t)
	cfg := config.ProviderConfig{
		APIKey: key,
		Model:  "google/gemini-2.0-flash-001",
	}
	return NewOpenRouter(cfg)
}

func TestIntegrationSimplePrompt(t *testing.T) {
	p := integrationProvider(t)
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{
			model.TextPart{Text: "What is 2+2? Reply with only the number."},
		}},
	}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	contentDeltaCount := 0
	completeCount := 0
	text := ""
	var usage *UsageInfo
	for _, e := range ev {
		if e.Type == EventContentDelta {
			contentDeltaCount++
			text += e.Delta
		}
		if e.Type == EventComplete {
			completeCount++
			usage = e.Usage
		}
	}
	if contentDeltaCount == 0 {
		t.Fatal("expected at least one EventContentDelta")
	}
	if completeCount != 1 {
		t.Fatalf("expected one EventComplete, got %d", completeCount)
	}
	if !strings.Contains(text, "4") {
		t.Fatalf("expected response to contain '4', got: %s", text)
	}
	if usage != nil && usage.PromptTokens == 0 {
		t.Fatalf("usage present but PromptTokens is 0: %#v", usage)
	}
}

func TestIntegrationToolCall(t *testing.T) {
	p := integrationProvider(t)
	tools := []ToolDef{{
		Name:        "get_weather",
		Description: "Get current weather",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"city": {"type": "string"}
			},
			"required": ["city"]
		}`),
	}}
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{
			model.TextPart{Text: "What is the weather in Paris? Use the get_weather tool."},
		}},
	}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, tools))
	var toolStart, toolStop, complete int
	toolName := ""
	for _, e := range ev {
		switch e.Type {
		case EventToolUseStart:
			toolStart++
			toolName = e.ToolName
		case EventToolUseStop:
			toolStop++
		case EventComplete:
			complete++
		}
	}
	if toolStart == 0 {
		t.Fatal("expected EventToolUseStart")
	}
	if toolName != "get_weather" {
		t.Fatalf("expected ToolName='get_weather', got: %s", toolName)
	}
	if toolStop == 0 {
		t.Fatal("expected EventToolUseStop")
	}
	if complete != 1 {
		t.Fatalf("expected one EventComplete, got %d", complete)
	}
}

func TestIntegrationStreamingDeltas(t *testing.T) {
	p := integrationProvider(t)
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{
			model.TextPart{Text: "Count from 1 to 10, one number per line."},
		}},
	}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	contentDeltaCount := 0
	for _, e := range ev {
		if e.Type == EventContentDelta {
			contentDeltaCount++
		}
	}
	if contentDeltaCount < 2 {
		t.Fatalf("expected multiple EventContentDelta for streaming, got %d", contentDeltaCount)
	}
}
