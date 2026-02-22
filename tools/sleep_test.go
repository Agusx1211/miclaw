package tools

import (
	"context"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func TestSleepToolDefinition(t *testing.T) {
	tl := sleepTool()
	if tl.Name() != "sleep" {
		t.Fatalf("tool name = %q", tl.Name())
	}
	if tl.Description() == "" {
		t.Fatal("tool description is empty")
	}
	if tl.Parameters().Type != "object" {
		t.Fatalf("tool parameters type = %q", tl.Parameters().Type)
	}
}

func TestSleepToolRun(t *testing.T) {
	got, err := sleepTool().Run(context.Background(), model.ToolCallPart{
		Name:       "sleep",
		Parameters: nil,
	})
	if err != nil {
		t.Fatalf("run sleep tool: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %q", got.Content)
	}
	if got.Content != "sleeping" {
		t.Fatalf("unexpected tool content: %q", got.Content)
	}
}
