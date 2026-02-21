package sandbox

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

type SSHExecutor struct {
	addr   string
	config *ssh.ClientConfig
}

func NewSSHExecutor(keyPath, user, host string) (*SSHExecutor, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		addr = net.JoinHostPort(host, "22")
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	return &SSHExecutor{addr: addr, config: cfg}, nil
}

func (e *SSHExecutor) Execute(ctx context.Context, command string) (string, int, error) {
	client, err := e.dial(ctx)
	if err != nil {
		return "", -1, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer session.Close()

	var output bytes.Buffer
	session.Stdout = &output
	session.Stderr = &output

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		return sshResult(output.String(), err)
	case <-ctx.Done():
		_ = session.Close()
		_ = client.Close()
		return output.String(), -1, ctx.Err()
	}
}

func IsHostCommand(command string, allowlist []string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	for _, allowed := range allowlist {
		if parts[0] == allowed {
			return true
		}
	}
	return false
}

func (e *SSHExecutor) dial(ctx context.Context) (*ssh.Client, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", e.addr)
	if err != nil {
		return nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, e.addr, e.config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func sshResult(output string, err error) (string, int, error) {
	if err == nil {
		return output, 0, nil
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return output, exitErr.ExitStatus(), nil
	}
	return output, -1, err
}
