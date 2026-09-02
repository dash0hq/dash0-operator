// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package kubectl

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestKubectlEnv(t *testing.T) {
	t.Run("redirects HOME to the configured writable directory", func(t *testing.T) {
		if home := effectiveHome(kubectlEnv("/tmp")); home != "/tmp" {
			t.Errorf("expected HOME to be redirected to /tmp, got %q", home)
		}
	})

	t.Run("preserves the ambient environment", func(t *testing.T) {
		t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
		if !slices.Contains(kubectlEnv("/tmp"), "KUBERNETES_SERVICE_HOST=10.0.0.1") {
			t.Error("expected the ambient KUBERNETES_SERVICE_HOST variable to be preserved")
		}
	})
}

// effectiveHome returns the value of the last HOME entry in env (the one exec uses), or "" if none is present.
func effectiveHome(env []string) string {
	home := ""
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, "HOME="); ok {
			home = rest
		}
	}
	return home
}

func TestCappedBuffer(t *testing.T) {
	t.Run("captures everything below the limit without truncating", func(t *testing.T) {
		b := &cappedBuffer{limit: 10}
		n, err := b.Write([]byte("hello"))
		if err != nil || n != 5 {
			t.Fatalf("expected Write to report 5 bytes and no error, got n=%d err=%v", n, err)
		}
		if b.String() != "hello" {
			t.Errorf("expected captured output %q, got %q", "hello", b.String())
		}
		if b.truncated {
			t.Error("expected truncated to be false")
		}
	})

	t.Run("caps captured output at the limit and reports truncation across writes", func(t *testing.T) {
		b := &cappedBuffer{limit: 4}
		// A single write past the limit keeps only the first limit bytes.
		if n, _ := b.Write([]byte("abcdef")); n != 6 {
			t.Fatalf("expected Write to report the full length 6, got %d", n)
		}
		// A subsequent write once full is fully discarded but still reported as written.
		if n, _ := b.Write([]byte("ghij")); n != 4 {
			t.Fatalf("expected Write to report the full length 4, got %d", n)
		}
		if b.String() != "abcd" {
			t.Errorf("expected captured output to be capped to %q, got %q", "abcd", b.String())
		}
		if !b.truncated {
			t.Error("expected truncated to be true after exceeding the limit")
		}
	})

	t.Run("does not report output of exactly the limit as truncated", func(t *testing.T) {
		b := &cappedBuffer{limit: 4}
		if _, err := b.Write([]byte("abcd")); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if b.truncated {
			t.Error("expected truncated to be false for output of exactly the limit")
		}
	})

	t.Run("caps stdout and stderr independently", func(t *testing.T) {
		// stderr gets a much smaller limit than stdout, so the two streams must not share one limit.
		if maxStderrBytes >= maxStdoutBytes {
			t.Fatalf("expected maxStderrBytes (%d) to be smaller than maxStdoutBytes (%d)",
				maxStderrBytes, maxStdoutBytes)
		}
		stdout := &cappedBuffer{limit: maxStdoutBytes}
		stderr := &cappedBuffer{limit: maxStderrBytes}
		payload := make([]byte, maxStderrBytes+1)
		if _, err := stdout.Write(payload); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, err := stderr.Write(payload); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if stdout.truncated {
			t.Error("expected stdout not to be truncated by an amount that only exceeds the stderr limit")
		}
		if !stderr.truncated {
			t.Error("expected stderr to be truncated at its own, smaller limit")
		}
	})
}

func TestWithTruncationNotice(t *testing.T) {
	// The notice names the limit of the stream it belongs to, so that stdout and stderr do not both report the stdout
	// limit.
	if notice := withTruncationNotice("out", maxStdoutBytes); !strings.Contains(notice, "3145728 bytes") {
		t.Errorf("expected the stdout notice to name maxStdoutBytes, got %q", notice)
	}
	if notice := withTruncationNotice("err", maxStderrBytes); !strings.Contains(notice, "262144 bytes") {
		t.Errorf("expected the stderr notice to name maxStderrBytes, got %q", notice)
	}
}

func TestExecuteCommandRequest(t *testing.T) {
	logger := discardLogger()

	t.Run("rejects an invalid command without executing it", func(t *testing.T) {
		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-1",
			Command:   "helm",
			Arguments: []string{"list"},
		})

		if resp.GetRequestId() != "req-1" {
			t.Errorf("expected the response to echo the request ID, got %q", resp.GetRequestId())
		}
		if resp.GetExitCode() != exitCodeRejected {
			t.Errorf("expected the rejected exit code %d, got %d", exitCodeRejected, resp.GetExitCode())
		}
		if !strings.Contains(resp.GetStderr(), "rejected the command") {
			t.Errorf("expected a rejection message on stderr, got %q", resp.GetStderr())
		}
		if resp.GetStdout() != "" {
			t.Errorf("expected no stdout for a rejected command, got %q", resp.GetStdout())
		}
		if resp.GetTimeout() {
			t.Error("expected timeout to be false for a rejected command")
		}
	})

	t.Run("executes an allowed command and reports its output and a zero exit code", func(t *testing.T) {
		// A fake "kubectl" that writes to stdout and stderr and exits successfully stands in for the real binary.
		fakeKubectlOnPath(t, "#!/bin/sh\necho stdout-line\necho stderr-line >&2\nexit 0\n")

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-ok",
			Command:   "kubectl",
			Arguments: []string{"get", "pods"},
		})

		if resp.GetRequestId() != "req-ok" {
			t.Errorf("expected the response to echo the request ID, got %q", resp.GetRequestId())
		}
		if resp.GetExitCode() != 0 {
			t.Errorf("expected exit code 0, got %d", resp.GetExitCode())
		}
		if resp.GetTimeout() {
			t.Error("expected timeout to be false for a successful command")
		}
		if strings.TrimSpace(resp.GetStdout()) != "stdout-line" {
			t.Errorf("expected stdout %q, got %q", "stdout-line", resp.GetStdout())
		}
		if strings.TrimSpace(resp.GetStderr()) != "stderr-line" {
			t.Errorf("expected stderr %q, got %q", "stderr-line", resp.GetStderr())
		}
	})

	t.Run("aborts a command that exceeds the timeout and reports the timeout response", func(t *testing.T) {
		// Put a fake "kubectl" that sleeps for 1s on PATH, then shorten the timeout to 10ms, so the invocation is
		// guaranteed to exceed the deadline and be killed.
		fakeKubectlOnPath(t, "#!/bin/sh\nsleep 1\n")
		setCommandTimeout(t, 10*time.Millisecond)

		resp := ExecuteCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
			RequestId: "req-timeout",
			Command:   "kubectl",
			Arguments: []string{"get", "pods"},
		})

		if resp.GetRequestId() != "req-timeout" {
			t.Errorf("expected the response to echo the request ID, got %q", resp.GetRequestId())
		}
		if !resp.GetTimeout() {
			t.Error("expected the timeout flag to be set")
		}
		if resp.GetExitCode() != exitCodeTimedOut {
			t.Errorf("expected the timeout exit code %d, got %d", exitCodeTimedOut, resp.GetExitCode())
		}
		if !strings.Contains(resp.GetStderr(), "aborted kubectl after the 10ms timeout") {
			t.Errorf("expected a timeout message mentioning the 10ms timeout on stderr, got %q", resp.GetStderr())
		}
	})
}

func TestPostProcessRunResult(t *testing.T) {
	t.Run("maps a nil error to exit code 0 with no message", func(t *testing.T) {
		code, msg := postProcessRunResult(nil, false)
		if code != 0 || msg != "" {
			t.Errorf("expected (0, \"\"), got (%d, %q)", code, msg)
		}
	})

	t.Run("maps a timed-out command to the timeout exit code with a message", func(t *testing.T) {
		// A timed-out command is killed by signal, so the error is an ExitError with code -1; the timedOut flag takes
		// precedence over the generic exit-error mapping.
		err := exec.Command("sh", "-c", "exit 7").Run()
		code, msg := postProcessRunResult(err, true)
		if code != exitCodeTimedOut {
			t.Errorf("expected the timeout exit code %d, got %d", exitCodeTimedOut, code)
		}
		if !strings.Contains(msg, "timeout") {
			t.Errorf("expected a timeout message, got %q", msg)
		}
	})

	t.Run("maps a process exit error to its exit code", func(t *testing.T) {
		err := exec.Command("sh", "-c", "exit 7").Run()
		if err == nil {
			t.Fatal("expected the subprocess to exit non-zero")
		}
		code, msg := postProcessRunResult(err, false)
		if code != 7 {
			t.Errorf("expected exit code 7, got %d", code)
		}
		if msg != "" {
			t.Errorf("expected no execution-failure message for a process that ran, got %q", msg)
		}
	})

	t.Run("maps a non-exit error to the not-executable code with a message", func(t *testing.T) {
		code, msg := postProcessRunResult(errors.New("boom"), false)
		if code != exitCodeNotExecutable {
			t.Errorf("expected the not-executable exit code %d, got %d", exitCodeNotExecutable, code)
		}
		if !strings.Contains(msg, "failed to execute kubectl") {
			t.Errorf("expected an execution-failure message, got %q", msg)
		}
	})
}

func TestAppendLine(t *testing.T) {
	if got := appendLine("", "line"); got != "line" {
		t.Errorf("expected %q, got %q", "line", got)
	}
	if got := appendLine("first", "second"); got != "first\nsecond" {
		t.Errorf("expected %q, got %q", "first\nsecond", got)
	}
}

// setCommandTimeout overrides the package-level commandTimeout for the duration of the test and restores it afterwards.
func setCommandTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := commandTimeout
	commandTimeout = d
	t.Cleanup(func() { commandTimeout = original })
}

// fakeKubectlOnPath installs a "kubectl" executable running the given shell script into a fresh temporary directory and
// prepends that directory to PATH, so that ExecuteCommandRequest's "kubectl" lookup resolves to the fake. t.Setenv
// restores PATH after the test.
func fakeKubectlOnPath(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake kubectl: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
