package tools

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestProcManagerReapsCompletedOnStart(t *testing.T) {
	mgr := NewProcManager()

	cmd1 := exec.Command("sh", "-c", "true")
	cmd1.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pid1 := mgr.Start(cmd1)
	waitProcDone(t, mgr, pid1)

	running, _, _, err := mgr.Status(pid1)
	if err != nil {
		t.Fatalf("status of completed process: %v", err)
	}
	if running {
		t.Fatal("expected process to be done")
	}

	// Starting a new process reaps completed entries.
	cmd2 := exec.Command("sh", "-c", "true")
	cmd2.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	mgr.Start(cmd2)

	if _, _, _, err := mgr.Status(pid1); err == nil {
		t.Fatal("expected reaped process to be removed")
	}
}

func TestProcManagerSignalKillsProcessGroup(t *testing.T) {
	mgr := NewProcManager()
	// Use a direct sleep (no shell background jobs) to avoid Go pipe
	// draining issues with inherited fds. This verifies Signal uses
	// syscall.Kill with negative PID for process group signaling.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pid := mgr.Start(cmd)

	time.Sleep(50 * time.Millisecond)
	if err := mgr.Signal(pid, syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	waitProcDone(t, mgr, pid)
}

func waitProcDone(t *testing.T, mgr *ProcManager, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		running, _, _, err := mgr.Status(pid)
		if err != nil {
			t.Fatalf("status of process %d: %v", pid, err)
		}
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d did not finish in time", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
