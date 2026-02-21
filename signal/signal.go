package signal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

type Envelope struct {
	SourceNumber string       `json:"sourceNumber"`
	SourceUUID   string       `json:"sourceUuid"`
	SourceName   string       `json:"sourceName"`
	Timestamp    int64        `json:"timestamp"`
	DataMessage  *DataMessage `json:"dataMessage"`
}

type DataMessage struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments"`
	Mentions    []Mention    `json:"mentions"`
	GroupInfo   *GroupInfo   `json:"groupInfo"`
	Reaction    *Reaction    `json:"reaction"`
	Timestamp   int64        `json:"timestamp"`
}

type Attachment struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Size        int    `json:"size"`
}

type Mention struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
}

type GroupInfo struct {
	GroupID string `json:"groupId"`
	Type    string `json:"type"`
}

type Reaction struct {
	Emoji      string `json:"emoji"`
	TargetUUID string `json:"targetAuthorUuid"`
	TargetTS   int64  `json:"targetSentTimestamp"`
	IsRemove   bool   `json:"isRemove"`
}

type TextStyle struct {
	Start  int
	Length int
	Style  string
}

func (s TextStyle) Encode() string {
	return fmt.Sprintf("%d:%d:%s", s.Start, s.Length, s.Style)
}

type Client struct {
	baseURL string
	account string
	http    *http.Client
}

func NewClient(baseURL, account string) *Client {
	return &Client{baseURL: baseURL, account: account, http: &http.Client{}}
}

func ParseEnvelope(data []byte) (*Envelope, error) {
	var wrapper struct {
		Envelope Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Envelope, nil
}

func SessionKey(env *Envelope) string {
	if env.DataMessage != nil && env.DataMessage.GroupInfo != nil {
		return "signal:group:" + env.DataMessage.GroupInfo.GroupID
	}
	return "signal:dm:" + env.SourceUUID
}

func IsSelfMessage(env *Envelope, account string) bool {
	return env.SourceNumber == account
}

func CheckAccess(policy string, allowlist []string, id string) bool {
	switch policy {
	case "open":
		return true
	case "disabled":
		return false
	case "allowlist":
		return slices.Contains(allowlist, id)
	}
	return false
}

var (
	reBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCode   = regexp.MustCompile("`([^`]+)`")
	reStrike = regexp.MustCompile(`~~(.+?)~~`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func MarkdownToSignal(md string) (string, []TextStyle) {
	s := md
	var styles []TextStyle

	s = reLink.ReplaceAllString(s, "$1 ($2)")
	s, styles = applyStyle(s, reBold, "BOLD", styles)
	s, styles = applyStyleItalic(s, styles)
	s, styles = applyStyle(s, reCode, "MONOSPACE", styles)
	s, styles = applyStyle(s, reStrike, "STRIKETHROUGH", styles)

	return s, styles
}

func applyStyle(s string, re *regexp.Regexp, style string, styles []TextStyle) (string, []TextStyle) {
	for {
		loc := re.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		inner := s[loc[2]:loc[3]]
		pos := loc[0]
		s = s[:loc[0]] + inner + s[loc[1]:]
		styles = append(styles, TextStyle{Start: pos, Length: len(inner), Style: style})
	}
	return s, styles
}

func applyStyleItalic(s string, styles []TextStyle) (string, []TextStyle) {
	for {
		idx := findSingleStar(s)
		if idx < 0 {
			break
		}
		end := findSingleStar(s[idx+1:])
		if end < 0 {
			break
		}
		end += idx + 1
		inner := s[idx+1 : end]
		s = s[:idx] + inner + s[end+1:]
		styles = append(styles, TextStyle{Start: idx, Length: len(inner), Style: "ITALIC"})
	}
	return s, styles
}

func findSingleStar(s string) int {
	for i := range len(s) {
		if s[i] != '*' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '*' {
			i++
			continue
		}
		if i > 0 && s[i-1] == '*' {
			continue
		}
		return i
	}
	return -1
}

func ChunkText(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		if len(text) <= limit {
			chunks = append(chunks, text)
			break
		}
		cut := limit
		if nl := strings.LastIndex(text[:limit], "\n"); nl > 0 {
			cut = nl + 1
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return chunks
}

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

func (c *Client) rpc(ctx context.Context, method string, params map[string]any) error {
	req := rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rpc %s: status %d: %s", method, resp.StatusCode, b)
	}
	return nil
}

func (c *Client) Send(ctx context.Context, recipient, message string, styles []TextStyle) error {
	params := map[string]any{
		"message":   message,
		"recipient": []string{recipient},
		"account":   c.account,
	}
	if len(styles) > 0 {
		encoded := make([]string, len(styles))
		for i, s := range styles {
			encoded[i] = s.Encode()
		}
		params["text-style"] = encoded
	}
	return c.rpc(ctx, "send", params)
}

func (c *Client) SendGroup(ctx context.Context, groupID, message string, styles []TextStyle) error {
	params := map[string]any{
		"message": message,
		"groupId": groupID,
		"account": c.account,
	}
	if len(styles) > 0 {
		encoded := make([]string, len(styles))
		for i, s := range styles {
			encoded[i] = s.Encode()
		}
		params["text-style"] = encoded
	}
	return c.rpc(ctx, "send", params)
}

func (c *Client) SendTyping(ctx context.Context, recipient string) error {
	return c.rpc(ctx, "sendTyping", map[string]any{
		"recipient": []string{recipient},
		"account":   c.account,
	})
}

func (c *Client) Listen(ctx context.Context) <-chan *Envelope {
	out := make(chan *Envelope, 16)
	go c.listenSSE(ctx, out)
	return out
}

func (c *Client) listenSSE(ctx context.Context, out chan<- *Envelope) {
	defer close(out)
	url := c.baseURL + "/api/v1/events?account=" + c.account
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		env, err := ParseEnvelope([]byte(data))
		if err != nil {
			continue
		}
		select {
		case out <- env:
		case <-ctx.Done():
			return
		}
	}
}
