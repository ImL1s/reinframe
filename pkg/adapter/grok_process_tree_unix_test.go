//go:build unix

package adapter_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// TestGrokACP_ProcessTreeReaped_Unix spawns a parent that starts a long-lived child,
// then proves forced Close reaps both (process group kill).
func TestGrokACP_ProcessTreeReaped_Unix(t *testing.T) {
	dir := t.TempDir()
	childPIDPath := filepath.Join(dir, "child.pid")
	src := filepath.Join(dir, "tree.go")
	// Parent: start a sleep child in the same process group (inherits), write child pid, then sleep.
	body := `package main
import (
  "fmt"
  "os"
  "os/exec"
  "syscall"
  "time"
)
func main() {
  // Child sleep — same process group as parent (default).
  c := exec.Command("sleep", "120")
  c.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
  if err := c.Start(); err != nil {
    panic(err)
  }
  _ = os.WriteFile(os.Getenv("CHILD_PID_PATH"), []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644)
  // Keep parent alive; ignore stdin EOF briefly.
  time.Sleep(120 * time.Second)
  _ = c.Wait()
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "tree-parent")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := adapter.StartGrokACPClient(ctx, adapter.GrokACPConfig{
		Executable: bin,
		Args:       []string{}, // not used — override via empty default? Start requires args without shell
		Env:        []string{"CHILD_PID_PATH=" + childPIDPath},
		// Force custom args empty → DefaultGrokACPArgs which this binary ignores as argv still passed.
		// Our fake ignores args; just needs to start.
	})
	// StartGrokACPClient always appends DefaultGrokACPArgs when Args empty — our binary ignores them.
	// But DefaultGrokACPArgs is non-empty strings as argv — Go main ignores extra args. OK.
	if err != nil {
		// Rebuild approach: pass Args explicitly as empty slice means default.
		// If start fails, report.
		t.Fatalf("start: %v", err)
	}
	// Wait for child pid file.
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
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
	parentPID := 0
	// Access via kill 0 probe after close — capture parent from /proc style: use process alive check.
	// We don't export cmd.Process; instead after Close both should be dead.
	// Signal probe: syscall.Kill(pid, 0)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	// Give the kernel a moment.
	time.Sleep(200 * time.Millisecond)
	if processAlive(childPID) {
		t.Fatalf("child pid %d still alive after Close (process tree not reaped)", childPID)
	}
	// Parent should also be gone; we don't have its pid exported — StartGrokACPClient Waited it.
	_ = parentPID
}

func TestGrokHeadless_ProcessTreeReaped_Unix(t *testing.T) {
	dir := t.TempDir()
	childPIDPath := filepath.Join(dir, "hchild.pid")
	src := filepath.Join(dir, "htree.go")
	body := `package main
import (
  "fmt"
  "os"
  "os/exec"
  "time"
)
func main() {
  c := exec.Command("sleep", "120")
  if err := c.Start(); err != nil { panic(err) }
  _ = os.WriteFile(os.Getenv("CHILD_PID_PATH"), []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644)
  // Emit minimal stream then hang so force kill is needed for the child tree.
  fmt.Println("{\"type\":\"init\"}")
  time.Sleep(120 * time.Second)
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "htree")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	// Short timeout so RunGrokHeadlessObserve escalates force kill.
	_, _ = adapter.RunGrokHeadlessObserve(context.Background(), adapter.GrokHeadlessObserveConfig{
		Executable: bin,
		Prompt:     "x",
		Timeout:    2 * time.Second,
		Env:        []string{"CHILD_PID_PATH=" + childPIDPath},
	})
	// Read child pid (may have been written).
	var childPID int
	deadline := time.Now().Add(3 * time.Second)
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
		t.Fatal("child pid not recorded")
	}
	time.Sleep(300 * time.Millisecond)
	if processAlive(childPID) {
		// Force kill leftover for hygiene then fail.
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatalf("headless child pid %d still alive", childPID)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}
