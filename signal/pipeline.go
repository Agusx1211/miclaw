package signal

import (
	"context"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/config"
)

type EnqueueFunc func(sessionID, content string, metadata map[string]string)

type Event struct {
	SessionID string
	Text      string
}

type SubscribeFunc func() (<-chan Event, func())

type Pipeline struct {
	client    *Client
	cfg       config.SignalConfig
	enqueue   EnqueueFunc
	subscribe SubscribeFunc
}

func NewPipeline(
	client *Client,
	cfg config.SignalConfig,
	enqueue EnqueueFunc,
	subscribe SubscribeFunc,
) *Pipeline {
	return &Pipeline{
		client:    client,
		cfg:       cfg,
		enqueue:   enqueue,
		subscribe: subscribe,
	}
}

func (p *Pipeline) Start(ctx context.Context) error {
	events, unsubscribe := p.subscribe()
	defer unsubscribe()

	typing := map[string]context.CancelFunc{}
	stopTyping := func() {
		for session, cancel := range typing {
			cancel()
			delete(typing, session)
		}
	}
	defer stopTyping()

	for envCh, evCh := p.client.Listen(ctx), events; ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-envCh:
			if !ok {
				return ctx.Err()
			}
			if IsSelfMessage(env, p.cfg.Account) {
				continue
			}
			if env.DataMessage == nil {
				continue
			}
			policy := p.cfg.DMPolicy
			subject := env.SourceUUID
			if env.DataMessage.GroupInfo != nil {
				policy = p.cfg.GroupPolicy
				subject = env.DataMessage.GroupInfo.GroupID
			}
			if !CheckAccess(policy, p.cfg.Allowlist, subject) {
				continue
			}
			p.startTyping(ctx, SessionKey(env), typing)
			content := renderMentions(env.DataMessage.Message, env.DataMessage.Mentions)
			p.enqueue(SessionKey(env), content, map[string]string{
				"source_name":   env.SourceName,
				"source_number": env.SourceNumber,
				"source_uuid":   env.SourceUUID,
			})
		case ev, ok := <-evCh:
			if !ok {
				return nil
			}
			if err := p.sendResponse(ctx, ev, typing); err != nil {
				return err
			}
		}
	}
}

func (p *Pipeline) sendResponse(ctx context.Context, ev Event, typing map[string]context.CancelFunc) error {
	recipient, kind, ok := parseSession(ev.SessionID)
	if !ok {
		return nil
	}
	if cancel, ok := typing[ev.SessionID]; ok {
		cancel()
		delete(typing, ev.SessionID)
	}
	text, styles := MarkdownToSignal(ev.Text)
	limit := p.cfg.TextChunkLimit
	if limit <= 0 {
		limit = len(text)
	}
	for _, chunk := range ChunkText(text, limit) {
		var err error
		if kind == "group" {
			err = p.client.SendGroup(ctx, recipient, chunk, styles)
		} else {
			err = p.client.Send(ctx, recipient, chunk, styles)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) startTyping(ctx context.Context, session string, typing map[string]context.CancelFunc) {
	if _, ok := typing[session]; ok {
		return
	}
	recipient, _, ok := parseSession(session)
	if !ok {
		return
	}
	typingCtx, cancel := context.WithCancel(ctx)
	typing[session] = cancel
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			_ = p.client.SendTyping(typingCtx, recipient)
			select {
			case <-ticker.C:
			case <-typingCtx.Done():
				return
			}
		}
	}()
}

func renderMentions(text string, mentions []Mention) string {
	for i := len(mentions) - 1; i >= 0; i-- {
		m := mentions[i]
		text = text[:m.Start] + "@" + m.Name + text[m.Start+m.Length:]
	}
	return text
}

func parseSession(sessionID string) (string, string, bool) {
	parts := strings.SplitN(sessionID, ":", 3)
	if len(parts) != 3 || parts[0] != "signal" {
		return "", "", false
	}
	return parts[2], parts[1], true
}
