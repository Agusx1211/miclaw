package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tools"
)

type bridgeStubTool struct {
	name string
}

func (t bridgeStubTool) Name() string { return t.name }

func (t bridgeStubTool) Description() string { return "" }

func (t bridgeStubTool) Parameters() tools.JSONSchema { return tools.JSONSchema{Type: "object"} }

func (t bridgeStubTool) Run(context.Context, model.ToolCallPart) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func TestWrapToolsWithSandboxBridgeWrapsBridgeableAndDropsProcess(t *testing.T) {
	bridge := &sandboxBridge{containerID: "abc123"}
	toolList := []tools.Tool{
		bridgeStubTool{name: "read"},
		bridgeStubTool{name: "process"},
		bridgeStubTool{name: "message"},
	}
	got := wrapToolsWithSandboxBridge(toolList, bridge)
	if len(got) != 2 {
		t.Fatalf("wrapped tool count = %d", len(got))
	}
	if _, ok := got[0].(sandboxProxyTool); !ok {
		t.Fatalf("first tool should be sandbox proxy, got %T", got[0])
	}
	if got[1].Name() != "message" {
		t.Fatalf("second tool name = %q", got[1].Name())
	}
}

func TestExecBackgroundRequested(t *testing.T) {
	if !execBackgroundRequested(json.RawMessage(`{"background":true}`)) {
		t.Fatal("expected background=true")
	}
	if execBackgroundRequested(json.RawMessage(`{"background":false}`)) {
		t.Fatal("expected background=false")
	}
}

func TestDockerRunContainerIDParsesLastLine(t *testing.T) {
	id, err := dockerRunContainerID([]byte("abc123def456\n"))
	if err != nil {
		t.Fatalf("parse container id: %v", err)
	}
	if id != "abc123def456" {
		t.Fatalf("container id = %q", id)
	}
}

func TestDockerRunContainerIDParsesAfterPullLogs(t *testing.T) {
	out := strings.Join([]string{
		"Unable to find image 'alpine:3.21' locally",
		"3.21: Pulling from library/alpine",
		"Status: Downloaded newer image for alpine:3.21",
		"f57ad5dd1825b6f972ca0f7991d29f94963db7048f923b02d22898b944f601f4",
	}, "\n")
	id, err := dockerRunContainerID([]byte(out))
	if err != nil {
		t.Fatalf("parse container id with pull logs: %v", err)
	}
	if id != "f57ad5dd1825b6f972ca0f7991d29f94963db7048f923b02d22898b944f601f4" {
		t.Fatalf("container id = %q", id)
	}
}

func TestDockerRunContainerIDRejectsNonIDLastLine(t *testing.T) {
	_, err := dockerRunContainerID([]byte("Status: Downloaded newer image for alpine:3.21\n"))
	if err == nil {
		t.Fatal("expected invalid container id error")
	}
}
