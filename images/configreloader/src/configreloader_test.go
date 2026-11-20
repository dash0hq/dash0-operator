// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
)

const testCollectorPid = 42

type testEnvironment struct {
	configFilePath string
	pidFilePath    string
	signalledPids  []int
	signalError    error
}

type fakeHealthCheck struct {
	url      string
	ready    atomic.Bool
	requests atomic.Int32
}

func newTestEnvironment(t *testing.T) *testEnvironment {
	t.Helper()
	logger = slog.New(slog.DiscardHandler)
	initializeSelfMonitoringMetrics(noop.NewMeterProvider().Meter("test"))
	configurationFileHashes = make(map[string]string)

	dir := t.TempDir()
	env := &testEnvironment{
		configFilePath: filepath.Join(dir, "config.yaml"),
		pidFilePath:    filepath.Join(dir, "pid.file"),
	}
	writeFile(t, env.configFilePath, []byte("version: 1"))
	writeFile(t, env.pidFilePath, []byte(strconv.Itoa(testCollectorPid)))
	if err := initializeHashes([]string{env.configFilePath}); err != nil {
		t.Fatalf("cannot initialize hashes: %v", err)
	}

	originalSignalCollector := signalCollector
	signalCollector = func(collectorPid int) error {
		if env.signalError != nil {
			return env.signalError
		}
		env.signalledPids = append(env.signalledPids, collectorPid)
		return nil
	}
	t.Cleanup(func() {
		signalCollector = originalSignalCollector
	})
	return env
}

func (env *testEnvironment) check(t *testing.T, collectorHealthCheckUrl string) (HasTriggeredReload, error) {
	t.Helper()
	return checkConfiguration(t.Context(), []string{env.configFilePath}, "", env.pidFilePath, collectorHealthCheckUrl)
}

func newFakeHealthCheck(t *testing.T, ready bool) *fakeHealthCheck {
	t.Helper()
	healthCheck := &fakeHealthCheck{}
	healthCheck.ready.Store(ready)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthCheck.requests.Add(1)
		if healthCheck.ready.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	healthCheck.url = server.URL + "/"
	return healthCheck
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

func gzipped(t *testing.T, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("cannot compress content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("cannot compress content: %v", err)
	}
	return buffer.Bytes()
}

func expectReloadTriggered(t *testing.T, triggered HasTriggeredReload, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !triggered {
		t.Fatal("expected a reload to be triggered")
	}
}

func expectNoReloadTriggered(t *testing.T, triggered HasTriggeredReload, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if triggered {
		t.Fatal("expected no reload to be triggered")
	}
}

func expectReloadFailed(t *testing.T, triggered HasTriggeredReload, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, errCollectorNotReady) {
		t.Fatalf("expected an error other than errCollectorNotReady, got: %v", err)
	}
	if triggered {
		t.Fatal("expected no reload to be triggered")
	}
}

func expectReloadDeferred(t *testing.T, triggered HasTriggeredReload, err error) {
	t.Helper()
	if !errors.Is(err, errCollectorNotReady) {
		t.Fatalf("expected errCollectorNotReady, got: %v", err)
	}
	if triggered {
		t.Fatal("expected no reload to be triggered")
	}
}

func expectSignalledPids(t *testing.T, env *testEnvironment, expected ...int) {
	t.Helper()
	if !slices.Equal(env.signalledPids, expected) {
		t.Fatalf("expected the collector pids %v to have been signalled, got: %v", expected, env.signalledPids)
	}
}

func TestCheckConfigurationDoesNotCheckReadinessWithoutChanges(t *testing.T) {
	env := newTestEnvironment(t)
	healthCheck := newFakeHealthCheck(t, true)

	triggered, err := env.check(t, healthCheck.url)

	expectNoReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env)
	if requests := healthCheck.requests.Load(); requests != 0 {
		t.Fatalf("expected no health check requests, got %d", requests)
	}
}

func TestCheckConfigurationDefersReloadUntilCollectorIsReady(t *testing.T) {
	env := newTestEnvironment(t)
	healthCheck := newFakeHealthCheck(t, false)
	writeFile(t, env.configFilePath, []byte("version: 2"))

	triggered, err := env.check(t, healthCheck.url)
	expectReloadDeferred(t, triggered, err)
	triggered, err = env.check(t, healthCheck.url)
	expectReloadDeferred(t, triggered, err)
	expectSignalledPids(t, env)

	healthCheck.ready.Store(true)
	triggered, err = env.check(t, healthCheck.url)
	expectReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)

	triggered, err = env.check(t, healthCheck.url)
	expectNoReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)
}

func TestCheckConfigurationDefersReloadWhenHealthCheckIsUnreachable(t *testing.T) {
	env := newTestEnvironment(t)
	server := httptest.NewServer(http.NotFoundHandler())
	unreachableUrl := server.URL + "/"
	server.Close()
	writeFile(t, env.configFilePath, []byte("version: 2"))

	triggered, err := env.check(t, unreachableUrl)

	expectReloadDeferred(t, triggered, err)
	expectSignalledPids(t, env)
}

func TestCheckConfigurationTriggersReloadWithoutHealthCheckUrl(t *testing.T) {
	env := newTestEnvironment(t)
	writeFile(t, env.configFilePath, []byte("version: 2"))

	triggered, err := env.check(t, "")

	expectReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)
}

func TestCheckConfigurationRetriesReloadAfterPidFileError(t *testing.T) {
	env := newTestEnvironment(t)
	healthCheck := newFakeHealthCheck(t, true)
	if err := os.Remove(env.pidFilePath); err != nil {
		t.Fatalf("cannot remove pid file: %v", err)
	}
	writeFile(t, env.configFilePath, []byte("version: 2"))

	triggered, err := env.check(t, healthCheck.url)
	expectReloadFailed(t, triggered, err)
	expectSignalledPids(t, env)

	writeFile(t, env.pidFilePath, []byte(strconv.Itoa(testCollectorPid)))
	triggered, err = env.check(t, healthCheck.url)
	expectReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)
}

func TestCheckConfigurationRetriesReloadAfterSignalError(t *testing.T) {
	env := newTestEnvironment(t)
	healthCheck := newFakeHealthCheck(t, true)
	env.signalError = errors.New("no such process")
	writeFile(t, env.configFilePath, []byte("version: 2"))

	triggered, err := env.check(t, healthCheck.url)
	expectReloadFailed(t, triggered, err)

	env.signalError = nil
	triggered, err = env.check(t, healthCheck.url)
	expectReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)
}

func TestCheckConfigurationRetriesDecompressionAfterError(t *testing.T) {
	env := newTestEnvironment(t)
	healthCheck := newFakeHealthCheck(t, true)
	dir := t.TempDir()
	compressedConfigFilePath := filepath.Join(dir, "config-compressed.yaml")
	decompressedConfigFilePath := filepath.Join(dir, "config.yaml")
	writeFile(t, compressedConfigFilePath, gzipped(t, "version: 1"))
	configurationFileHashes = make(map[string]string)
	if err := initializeHashes([]string{compressedConfigFilePath}); err != nil {
		t.Fatalf("cannot initialize hashes: %v", err)
	}
	check := func() (HasTriggeredReload, error) {
		return checkConfiguration(
			t.Context(),
			[]string{compressedConfigFilePath},
			decompressedConfigFilePath,
			env.pidFilePath,
			healthCheck.url,
		)
	}

	writeFile(t, compressedConfigFilePath, []byte("not gzipped"))
	triggered, err := check()
	expectReloadFailed(t, triggered, err)
	expectSignalledPids(t, env)

	writeFile(t, compressedConfigFilePath, gzipped(t, "version: 2"))
	triggered, err = check()
	expectReloadTriggered(t, triggered, err)
	expectSignalledPids(t, env, testCollectorPid)
	decompressed, err := os.ReadFile(decompressedConfigFilePath)
	if err != nil {
		t.Fatalf("cannot read decompressed config file: %v", err)
	}
	if string(decompressed) != "version: 2" {
		t.Fatalf("unexpected decompressed config file content: %s", decompressed)
	}
}

func TestCollectorHealthCheckUrlFromEnv(t *testing.T) {
	testCases := []struct {
		name     string
		podIp    string
		port     string
		expected string
	}{
		{name: "IPv4", podIp: "10.0.0.7", port: "13133", expected: "http://10.0.0.7:13133/"},
		{name: "IPv6", podIp: "fd00::7", port: "13133", expected: "http://[fd00::7]:13133/"},
		{name: "pod IP missing", podIp: "", port: "13133", expected: ""},
		{name: "port missing", podIp: "10.0.0.7", port: "", expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(podIpEnvVarName, testCase.podIp)
			t.Setenv(collectorHealthCheckPortEnvVarName, testCase.port)
			if actual := collectorHealthCheckUrlFromEnv(); actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}
