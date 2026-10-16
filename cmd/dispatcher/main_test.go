package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the actual main in a subprocess so exit status and resource cleanup are tested.
func TestDispatcherProcess(t *testing.T) {
	if os.Getenv("HOOKRELAY_TEST_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"dispatcher"}, os.Args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing child arguments")
}
func TestOccupiedPortExitsWithFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDispatcherProcess$", "--", "--listen", listener.Addr().String(), "--db", filepath.Join(t.TempDir(), "test.db"), "--allow-loopback-test")
	cmd.Env = append(os.Environ(), "HOOKRELAY_TEST_CHILD=1", "HOOKRELAY_TOKEN=process-test-token", "HOOKRELAY_SECRET=process-test-secret", "HOOKRELAY_METRICS_TOKEN=")
	output, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "address already in use") {
		t.Fatalf("err=%v output=%s", err, output)
	}
}

func TestHelpExitsSuccessfully(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDispatcherProcess$", "--", "--help")
	cmd.Env = append(os.Environ(), "HOOKRELAY_TEST_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("help failed: %v %s", err, output)
	}
}
