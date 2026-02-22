package signal

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/agusx1211/miclaw/config"
)

type EnqueueFunc func(sessionID, content string, metadata map[string]string)

type Pipeline struct {
	client  *Client
	cfg     config.SignalConfig
	enqueue EnqueueFunc
}

func NewPipeline(client *Client, cfg config.SignalConfig, enqueue EnqueueFunc) *Pipeline {
	return &Pipeline{
		client:  client,
		cfg:     cfg,
		enqueue: enqueue,
	}
}

func (p *Pipeline) Start(ctx context.Context) error {
	for envCh := p.client.Listen(ctx); ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-envCh:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("signal events stream closed")
			}
			log.Printf(
				"[signal] event from=%s uuid=%s has_data=%t session=%s",
				env.SourceNumber,
				env.SourceUUID,
				env.DataMessage != nil,
				SessionKey(env),
			)
			if IsSelfMessage(env, p.cfg.Account) {
				log.Printf("[signal] drop reason=self_message from=%s", env.SourceNumber)
				continue
			}
			if env.DataMessage == nil {
				log.Printf("[signal] drop reason=no_data from=%s", env.SourceNumber)
				continue
			}
			if !allowSignalAccess(p.cfg, env) {
				log.Printf("[signal] drop reason=access from=%s dm_policy=%s group_policy=%s", env.SourceNumber, p.cfg.DMPolicy, p.cfg.GroupPolicy)
				continue
			}
			content := renderMentions(env.DataMessage.Message, env.DataMessage.Mentions)
			log.Printf("[signal] accept session=%s msg=%q", SessionKey(env), compactSignalLogText(content))
			p.enqueue(SessionKey(env), content, map[string]string{
				"source_name":   env.SourceName,
				"source_number": env.SourceNumber,
				"source_uuid":   env.SourceUUID,
			})
		}
	}
}

func compactSignalLogText(raw string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len(clean) <= 180 {
		return clean
	}
	return clean[:177] + "..."
}

func allowSignalAccess(cfg config.SignalConfig, env *Envelope) bool {
	if env.DataMessage.GroupInfo != nil {
		return CheckAccess(cfg.GroupPolicy, cfg.Allowlist, env.DataMessage.GroupInfo.GroupID)
	}
	if cfg.DMPolicy != "allowlist" {
		return CheckAccess(cfg.DMPolicy, cfg.Allowlist, "")
	}
	return slices.Contains(cfg.Allowlist, env.SourceNumber) || slices.Contains(cfg.Allowlist, env.SourceUUID)
}

func renderMentions(text string, mentions []Mention) string {
	for i := len(mentions) - 1; i >= 0; i-- {
		m := mentions[i]
		text = text[:m.Start] + "@" + m.Name + text[m.Start+m.Length:]
	}
	return text
}
