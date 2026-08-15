package sandbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// runtime_test.go implements the Task 17 Step-4 vectors: real isolated
// execution (Docker when available, documented local fallback),
// limits, streaming output, forensic snapshots, timeout enforcement.

func TestLocalRuntimeExecutesAndSnapshots(t *testing.T) {
	rt := NewLocalRuntime()
	var lines []string
	var mu sync.Mutex
	code, err := rt.Run(context.Background(), RunSpec{
		RunID:   "r1",
		Command: []string{"sh", "-c", "echo hello; echo world"},
		Timeout: 10 * time.Second,
	}, func(stream, line string) {
		mu.Lock()
		lines = append(lines, stream+":"+line)
		mu.Unlock()
	})
	if err != nil || code != 0 {
		t.Fatalf("run: %v code=%d", err, code)
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "stdout:hello") {
		t.Fatalf("streamed lines = %v", lines)
	}
	snap, err := rt.Snapshot("r1")
	if err != nil || snap.ExitCode != 0 || snap.OutputSHA == "" {
		t.Fatalf("snapshot: %+v err=%v", snap, err)
	}
	// Unknown run has no snapshot.
	if _, err := rt.Snapshot("nope"); err == nil {
		t.Fatal("unknown run must not have a snapshot")
	}
}

func TestLocalRuntimeTimeoutAndExitCode(t *testing.T) {
	rt := NewLocalRuntime()
	code, err := rt.Run(context.Background(), RunSpec{
		RunID:   "r-timeout",
		Command: []string{"sh", "-c", "sleep 5"},
		Timeout: 200 * time.Millisecond,
	}, nil)
	// Context deadline kills the process — a non-zero exit or an error.
	if err == nil && code == 0 {
		t.Fatal("timeout must not produce a clean exit")
	}
	// Non-zero exit propagates the code.
	code2, err2 := rt.Run(context.Background(), RunSpec{
		RunID:   "r-exit",
		Command: []string{"sh", "-c", "exit 42"},
		Timeout: 5 * time.Second,
	}, nil)
	if err2 != nil || code2 != 42 {
		t.Fatalf("exit code = %d err=%v", code2, err2)
	}
	snap, _ := rt.Snapshot("r-exit")
	if snap.ExitCode != 42 {
		t.Fatalf("snapshot exit = %d", snap.ExitCode)
	}
}

func TestDockerRuntimeWhenAvailable(t *testing.T) {
	rt := NewDockerRuntime()
	if !rt.Available(context.Background()) {
		t.Skip("docker daemon unavailable — container vector skipped")
	}
	var lines []string
	code, err := rt.Run(context.Background(), RunSpec{
		RunID:       "e2e-sbx",
		Image:       "alpine:3.19",
		Command:     []string{"sh", "-c", "echo isolated"},
		CPULimit:    "0.5",
		MemoryLimit: "128m",
		NetworkOff:  true,
		Timeout:     60 * time.Second,
	}, func(stream, line string) {
		lines = append(lines, line)
	})
	if err != nil || code != 0 {
		t.Fatalf("docker run: %v code=%d", err, code)
	}
	// The first pull emits progress lines; the test only requires the
	// command's own output to arrive.
	found := false
	for _, l := range lines {
		if l == "isolated" {
			found = true
		}
	}
	if !found {
		t.Fatalf("command output missing: %v", lines)
	}
	snap, err := rt.Snapshot("e2e-sbx")
	if err != nil || snap.Image != "alpine:3.19" || snap.OutputSHA == "" {
		t.Fatalf("snapshot: %+v err=%v", snap, err)
	}
}
