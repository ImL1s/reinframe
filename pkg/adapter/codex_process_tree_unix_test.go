//go:build unix

package adapter_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestCodexAppServer_ProcessTreeReaped_Unix(t *testing.T) {
	dir := t.TempDir()
	childPIDPath := filepath.Join(dir, "codex_child.pid")
	src := filepath.Join(dir, "codex_tree.go")
	body := `package main
import (
  "bufio"
  "encoding/json"
  "fmt"
  "os"
  "os/exec"
  "syscall"
  "time"
)
func main() {
  c := exec.Command("sleep", "120")
  c.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
  if err := c.Start(); err != nil {
    panic(err)
  }
  _ = os.WriteFile(os.Getenv("CHILD_PID_PATH"), []byte(fmt.Sprintf("%d", c.Process.Pid)), 0o644)

  sc := bufio.NewScanner(os.Stdin)
  for sc.Scan() {
    var req map[string]any
    if json.Unmarshal(sc.Bytes(), &req) == nil {
      id, _ := req["id"].(float64)
      method, _ := req["method"].(string)
      if method == "initialize" {
        resp, _ := json.Marshal(map[string]any{
          "jsonrpc": "2.0",
          "id": int64(id),
          "result": map[string]any{
            "serverInfo": map[string]any{"name": "mock"},
            "protocolVersion": 1,
          },
        })
        os.Stdout.Write(append(resp, '\n'))
      }
    }
  }
  time.Sleep(120 * time.Second)
  _ = c.Wait()
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "codex_tree_parent")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client := adapter.NewCodexAppServerClient(adapter.CodexAppServerConfig{
		Executable:     bin,
		StartupTimeout: 10 * time.Second,
		Env:            []string{"CHILD_PID_PATH=" + childPIDPath},
	})
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

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
		_ = client.Close(ctx)
		t.Fatal("child pid not recorded")
	}

	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	if processAlive(childPID) {
		t.Fatalf("child pid %d still alive after Close (process tree not reaped)", childPID)
	}
}
