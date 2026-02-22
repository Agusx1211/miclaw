package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
)

func TestHostCommandServerRejectsDisallowedCommand(t *testing.T) {
	root := t.TempDir()
	sock := filepath.Join(root, "host-executor.sock")
	srv, err := startHostCommandServer(hostCommandServerConfig{
		SocketPath: sock,
		Workspace:  root,
		Allowed:    []string{"git"},
		Mounts: []config.Mount{
			{Host: root, Container: "/workspace", Mode: "rw"},
		},
	})
	if err != nil {
		t.Fatalf("start host command server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close host command server: %v", err)
		}
	})

	status, body, err := hostExecHTTPCall(sock, hostExecRequest{
		Command:    "docker",
		WorkingDir: "/workspace",
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("proxy call failed: %v", err)
	}
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, body=%q", status, body)
	}
	if !strings.Contains(body, "not allowed") {
		t.Fatalf("unexpected response body: %q", body)
	}
}

func TestHostCommandServerRunsAllowlistedCommandInMappedDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	sock := filepath.Join(root, "host-executor.sock")
	srv, err := startHostCommandServer(hostCommandServerConfig{
		SocketPath: sock,
		Workspace:  root,
		Allowed:    []string{"pwd"},
		Mounts: []config.Mount{
			{Host: root, Container: "/workspace", Mode: "rw"},
		},
	})
	if err != nil {
		t.Fatalf("start host command server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close host command server: %v", err)
		}
	})

	status, body, err := hostExecHTTPCall(sock, hostExecRequest{
		Command:    "pwd",
		WorkingDir: "/workspace/sub",
		TimeoutSec: 5,
	})
	if err != nil {
		t.Fatalf("proxy call failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%q", status, body)
	}
	var resp hostExecResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected server error: %q", resp.Error)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if strings.TrimSpace(resp.Stdout) != sub {
		t.Fatalf("stdout = %q, want %q", resp.Stdout, sub)
	}
}

func TestHostCommandServerForwardsInputToCommand(t *testing.T) {
	root := t.TempDir()
	sock := filepath.Join(root, "host-executor.sock")
	srv, err := startHostCommandServer(hostCommandServerConfig{
		SocketPath: sock,
		Workspace:  root,
		Allowed:    []string{"cat"},
		Mounts: []config.Mount{
			{Host: root, Container: "/workspace", Mode: "rw"},
		},
	})
	if err != nil {
		t.Fatalf("start host command server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("close host command server: %v", err)
		}
	})

	status, body, err := hostExecHTTPCall(sock, hostExecRequest{
		Command:    "cat",
		WorkingDir: "/workspace",
		TimeoutSec: 5,
		Input:      "violet bytes\nsilver bytes\n",
	})
	if err != nil {
		t.Fatalf("proxy call failed: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%q", status, body)
	}
	var resp hostExecResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", resp.ExitCode, resp.Stderr)
	}
	if resp.Stdout != "violet bytes\nsilver bytes\n" {
		t.Fatalf("stdout = %q", resp.Stdout)
	}
}

func TestRunHostExecClientStreamsAndReturnsExitCode(t *testing.T) {
	root := t.TempDir()
	sock := filepath.Join(root, "host-executor.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(sock)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		var req hostExecRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode client request: %v", err)
		}
		if req.Command != "git" {
			t.Fatalf("command = %q", req.Command)
		}
		if len(req.Args) != 1 || req.Args[0] != "status" {
			t.Fatalf("args = %#v", req.Args)
		}
		if req.Input != "morning drizzle\n" {
			t.Fatalf("input = %q", req.Input)
		}
		if req.WorkingDir == "" {
			t.Fatal("working dir is empty")
		}
		_ = json.NewEncoder(w).Encode(hostExecResponse{
			Stdout:   "ok-out\n",
			Stderr:   "warn\n",
			ExitCode: 4,
		})
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	t.Setenv(sandboxHostExecSocketEnv, sock)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = runHostExecClientWithInput(
		[]string{"git", "status"},
		strings.NewReader("morning drizzle\n"),
		false,
		&stdout,
		&stderr,
	)
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("expected exitCodeError, got %v", err)
	}
	if codeErr.Code != 4 {
		t.Fatalf("exit code = %d", codeErr.Code)
	}
	if stdout.String() != "ok-out\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "warn\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func hostExecHTTPCall(socketPath string, req hostExecRequest) (int, string, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 3 * time.Second,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, "", err
	}
	httpReq, err := http.NewRequest(http.MethodPost, "http://unix/execute", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}
