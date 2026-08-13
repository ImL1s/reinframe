//go:build windows

package adapter_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"golang.org/x/sys/windows"
)

// TestGrokACP_ProcessTreeReaped_Windows proves Job Object force-kill reaps parent+child.
func TestGrokACP_ProcessTreeReaped_Windows(t *testing.T) {
	dir := t.TempDir()
	childPIDPath := filepath.Join(dir, "child.pid")
	src := filepath.Join(dir, "tree.go")
	body := `package main
import (
  "fmt"
  "os"
  "os/exec"
  "time"
)
func main() {
  c := exec.Command("timeout", "/T", "120", "/NOBREAK")
  if err := c.Start(); err != nil {
    // fallback: ping loop
    c = exec.Command("ping", "-n", "120", "127.0.0.1")
    if err := c.Start(); err != nil { panic(err) }
  }
  _ = os.WriteFile(os.Getenv("CHILD_PID_PATH"), []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644)
  time.Sleep(120 * time.Second)
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "tree-parent.exe")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := adapter.StartGrokACPClient(ctx, adapter.GrokACPConfig{
		Executable: bin,
		Env:        []string{"CHILD_PID_PATH=" + childPIDPath},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	var childPID int
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(childPIDPath)
		if err == nil && len(b) > 0 {
			childPID, _ = strconv.Atoi(string(b))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if childPID <= 0 {
		_ = client.Close()
		t.Fatal("child pid not recorded")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if processAliveWindows(childPID) {
		t.Fatalf("child pid %d still alive after Job Object close", childPID)
	}
}

func TestGrokHeadless_ProcessTreeReaped_Windows(t *testing.T) {
	dir := t.TempDir()
	childPIDPath := filepath.Join(dir, "hchild.pid")
	src := filepath.Join(dir, "htree.go")
	// Write pid before streaming any stdout so Job-Object kill after first
	// JSON line cannot race out the pid file (CI flake under -race).
	body := `package main
import (
  "fmt"
  "os"
  "os/exec"
  "time"
)
func main() {
  c := exec.Command("ping", "-n", "120", "127.0.0.1")
  if err := c.Start(); err != nil {
    c = exec.Command("timeout", "/T", "120", "/NOBREAK")
    if err := c.Start(); err != nil { panic(err) }
  }
  path := os.Getenv("CHILD_PID_PATH")
  _ = os.WriteFile(path, []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644)
  // Ensure durable before any stdout that may trigger harness teardown.
  if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
    _ = f.Sync()
    _ = f.Close()
  }
  fmt.Println("{\"type\":\"init\"}")
  time.Sleep(120 * time.Second)
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "htree.exe")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Poll for child pid while observe runs (mirrors ACP test timing).
	type observeResult struct {
		err error
	}
	done := make(chan observeResult, 1)
	go func() {
		_, err := adapter.RunGrokHeadlessObserve(context.Background(), adapter.GrokHeadlessObserveConfig{
			Executable: bin,
			Prompt:     "x",
			Timeout:    8 * time.Second,
			Env:        []string{"CHILD_PID_PATH=" + childPIDPath},
		})
		done <- observeResult{err: err}
	}()

	var (
		childPID    int
		observeErr  error
		observeDone bool
	)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && childPID <= 0 {
		b, err := os.ReadFile(childPIDPath)
		if err == nil && len(b) > 0 {
			childPID, _ = strconv.Atoi(string(b))
			if childPID > 0 {
				break
			}
		}
		if !observeDone {
			select {
			case res := <-done:
				observeDone = true
				observeErr = res.err
			default:
			}
		}
		if observeDone && childPID <= 0 {
			t.Fatalf("child pid not recorded (observe finished early: %v)", observeErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if childPID <= 0 {
		if !observeDone {
			select {
			case res := <-done:
				t.Fatalf("child pid not recorded (observe err=%v)", res.err)
			case <-time.After(12 * time.Second):
				t.Fatal("child pid not recorded (observe still running)")
			}
		}
		t.Fatalf("child pid not recorded (observe err=%v)", observeErr)
	}
	// Wait for observe teardown (Job Object force kill).
	if !observeDone {
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("headless observe did not finish")
		}
	}
	time.Sleep(400 * time.Millisecond)
	if processAliveWindows(childPID) {
		t.Fatalf("headless child pid %d still alive", childPID)
	}
}

func processAliveWindows(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// STILL_ACTIVE = 259
	return code == 259
}
