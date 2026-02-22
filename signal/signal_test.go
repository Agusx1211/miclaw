package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseEnvelope(t *testing.T) {
	raw := `{
		"envelope": {
			"sourceNumber": "+15551234567",
			"sourceUuid": "abc-123",
			"sourceName": "Alice",
			"timestamp": 1700000000000,
			"dataMessage": {
				"message": "hello world",
				"timestamp": 1700000000000
			}
		}
	}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.SourceNumber != "+15551234567" {
		t.Fatalf("source number = %q", env.SourceNumber)
	}
	if env.SourceUUID != "abc-123" {
		t.Fatalf("source uuid = %q", env.SourceUUID)
	}
	if env.DataMessage == nil || env.DataMessage.Message != "hello world" {
		t.Fatalf("unexpected data message: %+v", env.DataMessage)
	}
}

func TestParseEnvelopeNoData(t *testing.T) {
	raw := `{"envelope": {"sourceNumber": "+1", "timestamp": 1}}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.DataMessage != nil {
		t.Fatalf("expected nil data message, got %+v", env.DataMessage)
	}
}

func TestParseEnvelopeGroupMessage(t *testing.T) {
	raw := `{
		"envelope": {
			"sourceNumber": "+15551234567",
			"sourceUuid": "abc",
			"timestamp": 1,
			"dataMessage": {
				"message": "hi group",
				"groupInfo": {
					"groupId": "grp-abc",
					"type": "DELIVER"
				}
			}
		}
	}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.DataMessage.GroupInfo == nil {
		t.Fatal("expected group info")
	}
	if env.DataMessage.GroupInfo.GroupID != "grp-abc" {
		t.Fatalf("group id = %q", env.DataMessage.GroupInfo.GroupID)
	}
}

func TestParseEnvelopeBare(t *testing.T) {
	raw := `{
		"sourceNumber": "+15551234567",
		"sourceUuid": "abc",
		"timestamp": 1,
		"dataMessage": {"message": "hi"}
	}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.SourceNumber != "+15551234567" {
		t.Fatalf("source number = %q", env.SourceNumber)
	}
	if env.DataMessage == nil || env.DataMessage.Message != "hi" {
		t.Fatalf("unexpected data message: %+v", env.DataMessage)
	}
}

func TestSessionKeyDM(t *testing.T) {
	env := &Envelope{SourceUUID: "user-123"}
	key := SessionKey(env)
	if key != "signal:dm:user-123" {
		t.Fatalf("got %q", key)
	}
}

func TestSessionKeyGroup(t *testing.T) {
	env := &Envelope{
		SourceUUID:  "user-123",
		DataMessage: &DataMessage{GroupInfo: &GroupInfo{GroupID: "grp-1"}},
	}
	key := SessionKey(env)
	if key != "signal:group:grp-1" {
		t.Fatalf("got %q", key)
	}
}

func TestCheckAccessDMOpen(t *testing.T) {
	if !CheckAccess("open", nil, "+1") {
		t.Fatal("open policy should allow")
	}
}

func TestCheckAccessDMDisabled(t *testing.T) {
	if CheckAccess("disabled", nil, "+1") {
		t.Fatal("disabled policy should deny")
	}
}

func TestCheckAccessDMAllowlistMatch(t *testing.T) {
	if !CheckAccess("allowlist", []string{"+1", "+2"}, "+1") {
		t.Fatal("should allow listed number")
	}
}

func TestCheckAccessDMAllowlistNoMatch(t *testing.T) {
	if CheckAccess("allowlist", []string{"+1"}, "+9") {
		t.Fatal("should deny unlisted number")
	}
}

func TestCheckAccessGroupOpen(t *testing.T) {
	if !CheckAccess("open", nil, "grp-1") {
		t.Fatal("open group policy should allow")
	}
}

func TestCheckAccessGroupDisabled(t *testing.T) {
	if CheckAccess("disabled", nil, "grp-1") {
		t.Fatal("disabled group policy should deny")
	}
}

func TestMarkdownToSignalBold(t *testing.T) {
	text, styles := MarkdownToSignal("hello **world**")
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 1 || styles[0].Style != "BOLD" || styles[0].Start != 6 || styles[0].Length != 5 {
		t.Fatalf("styles = %+v", styles)
	}
}

func TestMarkdownToSignalItalic(t *testing.T) {
	text, styles := MarkdownToSignal("hello *world*")
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 1 || styles[0].Style != "ITALIC" {
		t.Fatalf("styles = %+v", styles)
	}
}

func TestMarkdownToSignalCode(t *testing.T) {
	text, styles := MarkdownToSignal("use `fmt.Println`")
	if text != "use fmt.Println" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 1 || styles[0].Style != "MONOSPACE" {
		t.Fatalf("styles = %+v", styles)
	}
}

func TestMarkdownToSignalStrike(t *testing.T) {
	text, styles := MarkdownToSignal("~~removed~~")
	if text != "removed" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 1 || styles[0].Style != "STRIKETHROUGH" {
		t.Fatalf("styles = %+v", styles)
	}
}

func TestMarkdownToSignalLink(t *testing.T) {
	text, styles := MarkdownToSignal("see [docs](https://example.com)")
	if text != "see docs (https://example.com)" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 0 {
		t.Fatalf("styles = %+v", styles)
	}
}

func TestMarkdownToSignalPlainText(t *testing.T) {
	text, styles := MarkdownToSignal("hello world")
	if text != "hello world" || len(styles) != 0 {
		t.Fatalf("text = %q, styles = %+v", text, styles)
	}
}

func TestMarkdownToSignalMultiple(t *testing.T) {
	text, styles := MarkdownToSignal("**bold** and *italic*")
	if text != "bold and italic" {
		t.Fatalf("text = %q", text)
	}
	if len(styles) != 2 {
		t.Fatalf("expected 2 styles, got %d", len(styles))
	}
}

func TestChunkTextShort(t *testing.T) {
	chunks := ChunkText("short", 100)
	if len(chunks) != 1 || chunks[0] != "short" {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestChunkTextSplitsOnNewline(t *testing.T) {
	text := strings.Repeat("line\n", 20)
	chunks := ChunkText(text, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 30 {
			t.Fatalf("chunk too long: %d > 30", len(c))
		}
	}
}

func TestChunkTextLongLine(t *testing.T) {
	text := strings.Repeat("x", 50)
	chunks := ChunkText(text, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	joined := strings.Join(chunks, "")
	if joined != text {
		t.Fatal("chunks don't reconstruct original")
	}
}

func TestRPCSend(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	err := c.Send(context.Background(), "+15559999999", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}

	var req rpcRequest
	json.Unmarshal(gotBody, &req)
	if req.Method != "send" {
		t.Fatalf("method = %q", req.Method)
	}
}

func TestRPCSendGroup(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	err := c.SendGroup(context.Background(), "grp-1", "hello group", nil)
	if err != nil {
		t.Fatal(err)
	}

	var req rpcRequest
	json.Unmarshal(gotBody, &req)
	if req.Method != "send" {
		t.Fatalf("method = %q", req.Method)
	}
}

func TestRPCSendTyping(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		json.Unmarshal(body, &req)
		gotMethod = req.Method
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	err := c.SendTyping(context.Background(), "+15559999999")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "sendTyping" {
		t.Fatalf("method = %q", gotMethod)
	}
}

func TestRPCSendTypingStop(t *testing.T) {
	var gotReq rpcRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	err := c.SendTypingStop(context.Background(), "+15559999999")
	if err != nil {
		t.Fatal(err)
	}
	if gotReq.Method != "sendTyping" {
		t.Fatalf("method = %q", gotReq.Method)
	}
	stop, ok := gotReq.Params["stop"].(bool)
	if !ok || !stop {
		t.Fatalf("stop flag = %#v", gotReq.Params["stop"])
	}
}

func TestSSEListener(t *testing.T) {
	gotAccount := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccount <- r.URL.Query().Get("account")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		env := `{"envelope":{"sourceNumber":"+1","sourceUuid":"u1","timestamp":1,"dataMessage":{"message":"hi"}}}`
		fmt.Fprintf(w, "data: %s\n\n", env)
		flusher.Flush()
		// Close after one event
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Listen(ctx)
	select {
	case account := <-gotAccount:
		if account != "+15551234567" {
			t.Fatalf("account query = %q", account)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for account query")
	}
	select {
	case env := <-ch:
		if env.DataMessage == nil || env.DataMessage.Message != "hi" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestSSEListenerDataPrefixWithoutSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		env := `{"sourceNumber":"+1","sourceUuid":"u1","timestamp":1,"dataMessage":{"message":"hi"}}`
		fmt.Fprintf(w, "data:%s\n\n", env)
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Listen(ctx)
	select {
	case env := <-ch:
		if env.DataMessage == nil || env.DataMessage.Message != "hi" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestSSEListenerMultilineData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"sourceNumber\":\"+1\",\n")
		fmt.Fprint(w, "data: \"sourceUuid\":\"u1\",\"timestamp\":1,\"dataMessage\":{\"message\":\"hi\"}}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "+15551234567")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Listen(ctx)
	select {
	case env := <-ch:
		if env.DataMessage == nil || env.DataMessage.Message != "hi" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for event")
	}
}

func TestIsSelfMessage(t *testing.T) {
	env := &Envelope{SourceNumber: "+15551234567"}
	if !IsSelfMessage(env, "+15551234567") {
		t.Fatal("should detect self message")
	}
	if IsSelfMessage(env, "+15559999999") {
		t.Fatal("should not detect different sender as self")
	}
}

func TestTextStyleEncode(t *testing.T) {
	s := TextStyle{Start: 0, Length: 5, Style: "BOLD"}
	if s.Encode() != "0:5:BOLD" {
		t.Fatalf("got %q", s.Encode())
	}
}
