package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/agusx1211/miclaw/config"
)

type Server struct {
	server  *http.Server
	cfg     config.WebhookConfig
	enqueue func(source, content string, metadata map[string]string)
}

func New(cfg config.WebhookConfig, enqueue func(source, content string, metadata map[string]string)) *Server {
	s := &Server{
		cfg:     cfg,
		enqueue: enqueue,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	for _, hook := range cfg.Hooks {
		hook := hook
		mux.HandleFunc(hook.Path, s.webhookHandler(hook))
	}
	s.server = &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
	}
	return s
}

func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		if err := s.Stop(context.Background()); err != nil {
			return err
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (s *Server) Stop(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) webhookHandler(hook config.WebhookDef) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if hook.Secret != "" && !ValidateHMAC(body, r.Header.Get("X-Webhook-Signature"), hook.Secret) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		content := string(body)
		if hook.Format == "json" {
			content = string(body)
		}
		s.enqueue("webhook", "[webhook:"+hook.ID+"] "+content, map[string]string{"id": hook.ID})
		w.WriteHeader(http.StatusAccepted)
	}
}
