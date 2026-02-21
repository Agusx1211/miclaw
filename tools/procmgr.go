package tools

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const execProcOutputChars = 100000

type ProcManager struct {
	mu    sync.Mutex
	procs map[int]*managedProc
}

type managedProc struct {
	cmd       *exec.Cmd
	output    *bytes.Buffer
	startTime time.Time
	done      chan struct{}
	exitCode  int
}

type procOutputWriter struct {
	mgr  *ProcManager
	proc *managedProc
}

func NewProcManager() *ProcManager {
	return &ProcManager{procs: make(map[int]*managedProc)}
}

func (m *ProcManager) Start(cmd *exec.Cmd) int {
	proc := &managedProc{
		cmd:       cmd,
		output:    &bytes.Buffer{},
		startTime: time.Now(),
		done:      make(chan struct{}),
		exitCode:  -1,
	}
	out := &procOutputWriter{mgr: m, proc: proc}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		panic(err)
	}
	pid := cmd.Process.Pid
	m.mu.Lock()
	m.procs[pid] = proc
	m.mu.Unlock()

	go m.wait(pid)

	return pid
}

func (m *ProcManager) Status(pid int) (bool, int, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.procs[pid]
	if !ok {
		return false, 0, "", fmt.Errorf("process %d not found", pid)
	}

	return proc.exitCode == -1, proc.exitCode, proc.output.String(), nil
}

func (m *ProcManager) Signal(pid int, sig os.Signal) error {
	m.mu.Lock()
	proc, ok := m.procs[pid]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("process %d not found", pid)
	}
	return proc.cmd.Process.Signal(sig)
}

func (m *ProcManager) Poll(pid int) (string, error) {
	_, _, output, err := m.Status(pid)
	return output, err
}

func (m *ProcManager) wait(pid int) {
	proc, ok := m.getProc(pid)
	if !ok {
		return
	}
	_ = proc.cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	proc.exitCode = proc.cmd.ProcessState.ExitCode()
	close(proc.done)
}

func (m *ProcManager) getProc(pid int) (*managedProc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.procs[pid]
	return proc, ok
}

func (w *procOutputWriter) Write(p []byte) (int, error) {
	w.mgr.mu.Lock()
	defer w.mgr.mu.Unlock()
	capBuffer(w.proc.output, p, execProcOutputChars)
	return len(p), nil
}

func capBuffer(output *bytes.Buffer, input []byte, limit int) {
	if len(input) >= limit {
		output.Reset()
		output.Write(input[len(input)-limit:])
		return
	}
	if output.Len()+len(input) <= limit {
		output.Write(input)
		return
	}
	snapshot := output.Bytes()
	trim := output.Len() + len(input) - limit
	output.Reset()
	output.Write(snapshot[trim:])
	output.Write(input)
}
