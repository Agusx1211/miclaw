package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tools"
)

func runToolCall(encoded string, stdout io.Writer) error {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode tool call payload: %v", err)
	}
	var call model.ToolCallPart
	if err := json.Unmarshal(raw, &call); err != nil {
		return fmt.Errorf("parse tool call payload: %v", err)
	}
	t, ok := findBridgeTool(call.Name)
	if !ok {
		return fmt.Errorf("unsupported bridge tool %q", call.Name)
	}
	result, err := t.Run(context.Background(), call)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func findBridgeTool(name string) (tools.Tool, bool) {
	for _, t := range tools.BridgeTools() {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}
