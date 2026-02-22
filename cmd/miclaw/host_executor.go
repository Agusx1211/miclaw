package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/config"
)

const (
	hostExecDefaultTimeout = 1800
	hostExecEndpoint       = "http://unix/execute"
	hostExecMethod         = "/execute"
	hostExecTimeoutEnv     = "MICLAW_HOST_EXECUTOR_TIMEOUT"
)

type exitCodeError struct {
	Code    int
	Message string
}

func (e *exitCodeError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return fmt.Sprintf("exit code: %d", e.Code)
}

type hostExecRequest struct {
	Command     string   `json:"command"`
	Args        []string `json:"args,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	TimeoutSec  int      `json:"timeout_sec,omitempty"`
	ContainerID string   `json:"container_id,omitempty"`
}

type hostExecResponse struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

type hostExecErrorResponse struct {
	Error string `json:"error"`
}

type hostCommandServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
}

type hostCommandServerConfig struct {
	SocketPath string
	Workspace  string
	Allowed    []string
	Mounts     []config.Mount
	HostUser   string
}

type hostCommandHandler struct {
	allowed   map[string]bool
	mounts    []hostPathMount
	workspace string
	hostUser  string
}

type hostPathMount struct {
	container string
	host      string
}

func startHostCommandServer(cfg hostCommandServerConfig) (*hostCommandServer, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o755); err != nil {
		return nil, fmt.Errorf("create host command socket dir %q: %v", filepath.Dir(cfg.SocketPath), err)
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove host command socket %q: %v", cfg.SocketPath, err)
	}
	mounts, err := normalizeHostPathMounts(cfg.Workspace, cfg.Mounts)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen host command socket %q: %v", cfg.SocketPath, err)
	}
	if err := os.Chmod(cfg.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod host command socket %q: %v", cfg.SocketPath, err)
	}
	handler := hostCommandHandler{
		allowed:   hostAllowedSet(cfg.Allowed),
		mounts:    mounts,
		workspace: cfg.Workspace,
		hostUser:  cfg.HostUser,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(hostExecMethod, handler.handleExecute)
	server := &http.Server{Handler: mux}
	go serveHostCommandServer(server, ln)
	return &hostCommandServer{socketPath: cfg.SocketPath, listener: ln, server: server}, nil
}

func serveHostCommandServer(server *http.Server, ln net.Listener) {
	err := server.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[sandbox] host command server error: %v", err)
	}
}

func (s *hostCommandServer) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		firstErr = err
	}
	if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) && firstErr == nil {
		firstErr = err
	}
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func hostAllowedSet(commands []string) map[string]bool {
	allowed := map[string]bool{}
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		allowed[command] = true
	}
	return allowed
}

func normalizeHostPathMounts(workspace string, mounts []config.Mount) ([]hostPathMount, error) {
	out := []hostPathMount{{container: cleanContainerPath(workspace), host: workspace}}
	for _, mount := range mounts {
		hostPath, err := resolveBindPath(mount.Host)
		if err != nil {
			return nil, fmt.Errorf("resolve host mount path %q: %v", mount.Host, err)
		}
		out = append(out, hostPathMount{
			container: cleanContainerPath(mount.Container),
			host:      hostPath,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return len(out[i].container) > len(out[j].container)
	})
	return out, nil
}

func cleanContainerPath(raw string) string {
	clean := path.Clean(raw)
	if clean == "." {
		return "/"
	}
	return clean
}

func (h hostCommandHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeHostError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, err := decodeHostExecRequest(r)
	if err != nil {
		writeHostError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.allowed[req.Command] {
		writeHostError(w, http.StatusForbidden, fmt.Sprintf("host command %q is not allowed", req.Command))
		return
	}
	hostDir, err := h.resolveWorkingDir(req.WorkingDir)
	if err != nil {
		writeHostError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := runHostCommand(r.Context(), req, hostDir)
	if err != nil {
		writeHostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf(
		"[sandbox] host command container=%s user=%s cmd=%s args=%d exit=%d",
		req.ContainerID, h.hostUser, req.Command, len(req.Args), resp.ExitCode,
	)
	writeHostResponse(w, http.StatusOK, resp)
}

func decodeHostExecRequest(r *http.Request) (hostExecRequest, error) {
	var req hostExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return hostExecRequest{}, fmt.Errorf("decode request: %v", err)
	}
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		return hostExecRequest{}, fmt.Errorf("command is required")
	}
	if strings.Contains(req.Command, "/") || strings.ContainsAny(req.Command, " \t\r\n") {
		return hostExecRequest{}, fmt.Errorf("command must be a single command name")
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = hostExecDefaultTimeout
	}
	return req, nil
}

func (h hostCommandHandler) resolveWorkingDir(containerDir string) (string, error) {
	if strings.TrimSpace(containerDir) == "" {
		return h.workspace, nil
	}
	clean := cleanContainerPath(containerDir)
	if !strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("working_dir must be absolute")
	}
	for _, mount := range h.mounts {
		if !containerPathContains(clean, mount.container) {
			continue
		}
		suffix := strings.TrimPrefix(clean, mount.container)
		if suffix == "" {
			return mount.host, nil
		}
		return filepath.Join(mount.host, filepath.FromSlash(strings.TrimPrefix(suffix, "/"))), nil
	}
	return "", fmt.Errorf("working_dir %q is outside mounted paths", containerDir)
}

func containerPathContains(pathValue, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(pathValue, "/")
	}
	return pathValue == prefix || strings.HasPrefix(pathValue, prefix+"/")
}

func runHostCommand(ctx context.Context, req hostExecRequest, hostDir string) (hostExecResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, req.Command, req.Args...)
	command.Dir = hostDir
	command.Env = hostCommandEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return hostExecResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return hostExecResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitErr.ExitCode()}, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return hostExecResponse{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 124}, nil
	}
	return hostExecResponse{}, fmt.Errorf("execute command: %v", err)
}

func hostCommandEnv() []string {
	pathValue := os.Getenv("PATH")
	if pathValue == "" {
		pathValue = sandboxDefaultPATH
	}
	env := []string{"PATH=" + pathValue}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	if user := os.Getenv("USER"); user != "" {
		env = append(env, "USER="+user)
	}
	return env
}

func runHostExecClient(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("host exec client requires command")
	}
	req := hostExecRequest{
		Command:    args[0],
		Args:       args[1:],
		TimeoutSec: hostExecTimeout(),
	}
	if cwd, err := os.Getwd(); err == nil {
		req.WorkingDir = cwd
	}
	if host, err := os.Hostname(); err == nil {
		req.ContainerID = host
	}
	socketPath := os.Getenv(sandboxHostExecSocketEnv)
	if socketPath == "" {
		socketPath = sandboxHostExecSocketContPath
	}
	resp, status, body, err := doHostExecRequest(socketPath, req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.New(readHostExecError(body))
	}
	if _, err := io.WriteString(stdout, resp.Stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, resp.Stderr); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if resp.ExitCode != 0 {
		return &exitCodeError{Code: resp.ExitCode}
	}
	return nil
}

func hostExecTimeout() int {
	raw := strings.TrimSpace(os.Getenv(hostExecTimeoutEnv))
	if raw == "" {
		return hostExecDefaultTimeout
	}
	timeout, err := strconv.Atoi(raw)
	if err != nil || timeout <= 0 {
		return hostExecDefaultTimeout
	}
	return timeout
}

func doHostExecRequest(socketPath string, req hostExecRequest) (hostExecResponse, int, string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return hostExecResponse{}, 0, "", fmt.Errorf("marshal host exec request: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: time.Duration(req.TimeoutSec+5) * time.Second,
	}
	httpReq, err := http.NewRequest(http.MethodPost, hostExecEndpoint, bytes.NewReader(body))
	if err != nil {
		return hostExecResponse{}, 0, "", fmt.Errorf("build host exec request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return hostExecResponse{}, 0, "", fmt.Errorf("call host exec server: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return hostExecResponse{}, 0, "", fmt.Errorf("read host exec response: %v", err)
	}
	var out hostExecResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			return hostExecResponse{}, 0, "", fmt.Errorf("decode host exec response: %v", err)
		}
	}
	return out, resp.StatusCode, string(raw), nil
}

func readHostExecError(raw string) string {
	var msg hostExecErrorResponse
	if err := json.Unmarshal([]byte(raw), &msg); err == nil && strings.TrimSpace(msg.Error) != "" {
		return msg.Error
	}
	if strings.TrimSpace(raw) == "" {
		return "host exec request failed"
	}
	return strings.TrimSpace(raw)
}

func writeHostError(w http.ResponseWriter, status int, message string) {
	writeHostResponse(w, status, hostExecErrorResponse{Error: message})
}

func writeHostResponse(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
