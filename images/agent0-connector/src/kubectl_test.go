// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

//nolint:lll
func TestValidateCommandRequest(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		arguments []string
		allowed   bool
	}{
		{name: "no subcommand (bare kubectl) is allowed", command: "kubectl", arguments: nil, allowed: true},
		{name: "no subcommand (empty arguments) is allowed", command: "kubectl", arguments: []string{}, allowed: true},
		{name: "global help flag without subcommand is allowed", command: "kubectl", arguments: []string{"--help"}, allowed: true},

		{name: "read-only get is allowed", command: "kubectl", arguments: []string{"get", "pods"}, allowed: true},
		{name: "get with -n flag is allowed", command: "kubectl", arguments: []string{"get", "po", "-n", "x"}, allowed: true},
		{name: "describe is allowed", command: "kubectl", arguments: []string{"describe", "pod", "x"}, allowed: true},
		{name: "logs is allowed", command: "kubectl", arguments: []string{"logs", "x"}, allowed: true},
		{name: "version is allowed", command: "kubectl", arguments: []string{"version"}, allowed: true},
		{name: "explain is allowed", command: "kubectl", arguments: []string{"explain", "pods"}, allowed: true},
		{name: "api-resources is allowed", command: "kubectl", arguments: []string{"api-resources"}, allowed: true},
		{name: "api-versions is allowed", command: "kubectl", arguments: []string{"api-versions"}, allowed: true},
		{name: "cluster-info is allowed", command: "kubectl", arguments: []string{"cluster-info"}, allowed: true},
		{name: "top is allowed", command: "kubectl", arguments: []string{"top", "pods"}, allowed: true},
		{name: "events is allowed", command: "kubectl", arguments: []string{"events"}, allowed: true},

		{name: "non-kubectl command is rejected", command: "helm", arguments: []string{"list"}, allowed: false},
		{name: "empty command is rejected", command: "", arguments: []string{"get", "pods"}, allowed: false},
		{name: "mutating delete is rejected", command: "kubectl", arguments: []string{"delete", "pod", "x"}, allowed: false},
		{name: "mutating apply is rejected", command: "kubectl", arguments: []string{"apply", "-f", "x"}, allowed: false},
		{name: "mutating edit is rejected", command: "kubectl", arguments: []string{"edit", "deploy", "x"}, allowed: false},
		{name: "leading value-taking flag before subcommand is allowed", command: "kubectl", arguments: []string{"-n", "x", "get", "po"}, allowed: true},
		{name: "leading flag in --flag=value form before subcommand is allowed", command: "kubectl", arguments: []string{"--namespace=x", "get", "po"}, allowed: true},
		{name: "leading boolean flag before subcommand is allowed", command: "kubectl", arguments: []string{"-v=5", "get", "po"}, allowed: true},
		{name: "leading flags before a mutating subcommand are still rejected", command: "kubectl", arguments: []string{"-n", "x", "delete", "pod", "y"}, allowed: false},
		{name: "sensitive flag before subcommand is still rejected", command: "kubectl", arguments: []string{"--kubeconfig=/x", "get", "po"}, allowed: false},
		{name: "watch flag (-w) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-w"}, allowed: false},
		{name: "--watch is rejected", command: "kubectl", arguments: []string{"get", "po", "--watch"}, allowed: false},
		{name: "--watch-only is rejected", command: "kubectl", arguments: []string{"get", "pods", "--watch-only"}, allowed: false},
		{name: "--watch=true is rejected", command: "kubectl", arguments: []string{"get", "--watch=true"}, allowed: false},
		{name: "--watch-only=true is rejected", command: "kubectl", arguments: []string{"get", "--watch-only=true"}, allowed: false},

		{name: "follow flag (-f) is rejected", command: "kubectl", arguments: []string{"logs", "x", "-f"}, allowed: false},
		{name: "--follow is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow"}, allowed: false},
		{name: "--follow=true is rejected", command: "kubectl", arguments: []string{"logs", "x", "--follow=true"}, allowed: false},

		{name: "impersonation (--as) is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as", "system:admin"}, allowed: false},
		{name: "--as-group is rejected", command: "kubectl", arguments: []string{"get", "pods", "--as-group=system:masters"}, allowed: false},
		{name: "--server is rejected", command: "kubectl", arguments: []string{"get", "pods", "--server", "https://evil"}, allowed: false},
		{name: "-s (server short flag) is rejected", command: "kubectl", arguments: []string{"get", "pods", "-s", "https://evil"}, allowed: false},
		{name: "--kubeconfig is rejected", command: "kubectl", arguments: []string{"get", "pods", "--kubeconfig=/x"}, allowed: false},
		{name: "--context is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context", "other"}, allowed: false},
		{name: "--context=value is rejected", command: "kubectl", arguments: []string{"get", "pods", "--context=other"}, allowed: false},
		{name: "--token is rejected", command: "kubectl", arguments: []string{"get", "pods", "--token", "abc"}, allowed: false},
		{name: "--insecure-skip-tls-verify is rejected", command: "kubectl", arguments: []string{"get", "pods", "--insecure-skip-tls-verify"}, allowed: false},

		// Secrets: listing and presence checks are allowed, reading their contents is not.
		{name: "listing secrets is allowed",
			command: "kubectl", arguments: []string{"get", "secrets"}, allowed: true},
		{name: "listing secrets in a namespace is allowed",
			command: "kubectl", arguments: []string{"get", "secrets", "-n", "x"}, allowed: true},
		{name: "presence check of a secret is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret"}, allowed: true},
		{name: "presence check via type/name is allowed",
			command: "kubectl", arguments: []string{"get", "secret/my-secret"}, allowed: true},
		{name: "describe secret is allowed",
			command: "kubectl", arguments: []string{"describe", "secret", "my-secret"}, allowed: true},
		{name: "secret with -o name is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "name"}, allowed: true},
		{name: "secret with -o wide is allowed",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "wide"}, allowed: true},

		{name: "secret with -o yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "yaml"}, allowed: false},
		{name: "secret with -o json is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "json"}, allowed: false},
		{name: "secret with -ojson (combined) is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-ojson"}, allowed: false},
		{name: "secret with --output=yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--output=yaml"}, allowed: false},
		{name: "secret with -o jsonpath is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "my-secret", "-o", "jsonpath={.data}"}, allowed: false},
		{name: "secret with -o go-template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "go-template={{.data}}"}, allowed: false},
		{name: "secret with -o custom-columns is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "-o", "custom-columns=D:.data"}, allowed: false},
		{name: "secret with --template is rejected",
			command: "kubectl", arguments: []string{"get", "secret", "--template={{.data}}"}, allowed: false},
		{name: "listing secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets", "-o", "yaml"}, allowed: false},
		{name: "secret via type/name as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret/my-secret", "-o", "yaml"}, allowed: false},
		{name: "fully qualified secret as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secrets.v1.", "-o", "yaml"}, allowed: false},
		{name: "output flag before resource is rejected",
			command: "kubectl", arguments: []string{"get", "-o", "yaml", "secret", "my-secret"}, allowed: false},
		{name: "multi-resource list including secrets as yaml is rejected",
			command: "kubectl", arguments: []string{"get", "secret,pods", "-o", "yaml"}, allowed: false},
		{name: "secret as yaml with a leading flag is rejected",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret", "-o", "yaml"}, allowed: false},
		{name: "presence check of a secret with a leading flag is allowed",
			command: "kubectl", arguments: []string{"-n", "x", "get", "secret", "my-secret"}, allowed: true},

		// Non-secret resources are unaffected by the secret-content check.
		{name: "non-secret resource as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-o", "yaml"}, allowed: true},
		{name: "pod named secret as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "secret", "-o", "yaml"}, allowed: true},
		{name: "namespace named secret as yaml is allowed",
			command: "kubectl", arguments: []string{"get", "pods", "-n", "secret", "-o", "yaml"}, allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.CommandRequest{Command: tt.command, Arguments: tt.arguments}
			err := validateCommandRequest(req)
			if tt.allowed && err != nil {
				t.Errorf("expected request to be allowed, but it was rejected: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected request to be rejected, but it was allowed")
			}
		})
	}
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
}

func TestExecuteCommandRequest(t *testing.T) {
	logger := discardLogger()

	t.Run("rejects an invalid command without executing it", func(t *testing.T) {
		resp := executeCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
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

		resp := executeCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
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

		resp := executeCommandRequest(context.Background(), logger, "/tmp", &pb.CommandRequest{
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
// prepends that directory to PATH, so that executeCommandRequest's "kubectl" lookup resolves to the fake. t.Setenv
// restores PATH after the test.
func fakeKubectlOnPath(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake kubectl: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
