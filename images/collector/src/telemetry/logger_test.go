// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const testSecurityLabel = "system_u:system_r:container_t:s0:c1,c2"

// stubPodLogsDir replaces the pod log directory with an empty temporary directory for the duration of the test, lists
// it in the replaced mounts file when mounted is true, and returns it.
func stubPodLogsDir(t *testing.T, mounted bool) string {
	t.Helper()
	dir := t.TempDir()
	stubDir := t.TempDir()

	mounts := "proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"
	if mounted {
		mounts += "/dev/sda1 " + dir + " ext4 ro,relatime 0 0\n"
	}
	stubMountsFile := filepath.Join(stubDir, "mounts")
	writeFile(t, stubMountsFile, mounts)
	// The kernel terminates an SELinux context with a NUL byte.
	stubSecurityLabelFile := filepath.Join(stubDir, "current")
	writeFile(t, stubSecurityLabelFile, testSecurityLabel+"\x00")

	originalPodLogsDir, originalMountsFile, originalSecurityLabelFile := podLogsDir, mountsFile, securityLabelFile
	podLogsDir, mountsFile, securityLabelFile = dir, stubMountsFile, stubSecurityLabelFile
	t.Cleanup(func() {
		podLogsDir, mountsFile, securityLabelFile = originalPodLogsDir, originalMountsFile, originalSecurityLabelFile
	})
	return dir
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkPodLogsDir() *observer.ObservedLogs {
	core, logs := observer.New(zapcore.DebugLevel)
	warnIfNoPodLogFilesFound(zap.New(core))
	return logs
}

func expectOneWarning(t *testing.T, logs *observer.ObservedLogs) map[string]any {
	t.Helper()
	if logs.Len() != 1 {
		t.Fatalf("expected one log entry, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Level != zapcore.WarnLevel {
		t.Fatalf("expected a warning, got %s", entry.Level)
	}
	fields := entry.ContextMap()
	if fields["directory"] != podLogsDir {
		t.Errorf("expected directory %q, got %v", podLogsDir, fields["directory"])
	}
	if fields["uid"] != int64(os.Getuid()) {
		t.Errorf("expected uid %d, got %v", os.Getuid(), fields["uid"])
	}
	if fields["gid"] != int64(os.Getgid()) {
		t.Errorf("expected gid %d, got %v", os.Getgid(), fields["gid"])
	}
	if _, ok := fields["groups"]; !ok {
		t.Errorf("expected the groups to be logged")
	}
	if fields["security_label"] != testSecurityLabel {
		t.Errorf("expected security label %q, got %q", testSecurityLabel, fields["security_label"])
	}
	return fields
}

func TestWarnIfNoPodLogFilesFoundWarnsWhenTheDirectoryContainsNoLogFile(t *testing.T) {
	dir := stubPodLogsDir(t, true)
	// Rotated log files are not matched by the filelog receiver, so they do not count.
	writeFile(t, filepath.Join(dir, "default_app_uid", "app", "0.log.20260928-120000"), "")

	fields := expectOneWarning(t, checkPodLogsDir())
	if _, ok := fields["error"]; ok {
		t.Errorf("expected no error, got %v", fields["error"])
	}
}

// The directory is searchable but not readable, which is what an SELinux denial of the directory listing amounts to.
func TestWarnIfNoPodLogFilesFoundWarnsWhenTheDirectoryCannotBeListed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can list directories regardless of their permissions")
	}
	dir := stubPodLogsDir(t, true)
	writeFile(t, filepath.Join(dir, "default_app_uid", "app", "0.log"), "")
	if err := os.Chmod(dir, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	fields := expectOneWarning(t, checkPodLogsDir())
	if errorMessage, _ := fields["error"].(string); !strings.Contains(errorMessage, "permission denied") {
		t.Errorf("expected a permission denied error, got %v", fields["error"])
	}
}

func TestWarnIfNoPodLogFilesFoundDoesNotWarnWhenAnotherPodDirectoryCannotBeListed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can list directories regardless of their permissions")
	}
	dir := stubPodLogsDir(t, true)
	// The walk visits the directories in lexical order, so the unlistable one comes first.
	unlistablePodDir := filepath.Join(dir, "a_app_uid")
	writeFile(t, filepath.Join(unlistablePodDir, "app", "0.log"), "")
	writeFile(t, filepath.Join(dir, "b_app_uid", "app", "0.log"), "")
	if err := os.Chmod(unlistablePodDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unlistablePodDir, 0o755) })

	if logs := checkPodLogsDir(); logs.Len() != 0 {
		t.Fatalf("expected no log entry, got %d", logs.Len())
	}
}

func TestWarnIfNoPodLogFilesFoundDoesNotWarnWhenALogFileIsFound(t *testing.T) {
	dir := stubPodLogsDir(t, true)
	writeFile(t, filepath.Join(dir, "default_app_uid", "app", "0.log"), "")

	if logs := checkPodLogsDir(); logs.Len() != 0 {
		t.Fatalf("expected no log entry, got %d", logs.Len())
	}
}

func TestWarnIfNoPodLogFilesFoundDoesNotWarnWhenTheDirectoryIsNotMounted(t *testing.T) {
	stubPodLogsDir(t, false)

	if logs := checkPodLogsDir(); logs.Len() != 0 {
		t.Fatalf("expected no log entry, got %d", logs.Len())
	}
}
