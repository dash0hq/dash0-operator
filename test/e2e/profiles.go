// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
)

const (
	ebpfProfilerImage = "otel/opentelemetry-collector-ebpf-profiler:0.148.0"
)

func deployEbpfProfiler(operatorNs string) {
	By("deploying eBPF profiler")
	collectorServiceEndpoint := fmt.Sprintf(
		"%s-opentelemetry-collector-service.%s.svc.cluster.local:4317",
		operatorHelmReleaseName,
		operatorNs,
	)

	manifest := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: ebpf-profiler-config
  namespace: %s
data:
  config.yaml: |
    receivers:
      profiling:

    exporters:
      otlp/collector:
        endpoint: %s
        tls:
          insecure: true

    service:
      telemetry:
        logs:
          level: info
      pipelines:
        profiles:
          receivers: [profiling]
          exporters: [otlp/collector]
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ebpf-profiler
  namespace: %s
  labels:
    app: ebpf-profiler
    dash0.com/enable: "false"
spec:
  selector:
    matchLabels:
      app: ebpf-profiler
  template:
    metadata:
      labels:
        app: ebpf-profiler
        dash0.com/enable: "false"
    spec:
      hostPID: true
      containers:
        - name: ebpf-profiler
          image: %s
          args:
            - --config=file:/etc/otelcol/config.yaml
            - --feature-gates=service.profilesSupport
          securityContext:
            capabilities:
              add:
                - SYS_ADMIN
                - SYS_PTRACE
                - SYS_RESOURCE
                - SYSLOG
          volumeMounts:
            - name: config
              mountPath: /etc/otelcol
            - name: proc
              mountPath: /proc
              readOnly: true
            - name: sys
              mountPath: /sys
              readOnly: true
      volumes:
        - name: config
          configMap:
            name: ebpf-profiler-config
        - name: proc
          hostPath:
            path: /proc
        - name: sys
          hostPath:
            path: /sys
`, operatorNs, collectorServiceEndpoint, operatorNs, ebpfProfilerImage)

	tmpFile := filepath.Join(os.TempDir(), "ebpf-profiler-manifest.yaml")
	err := os.WriteFile(tmpFile, []byte(manifest), 0600)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = os.Remove(tmpFile)
	}()

	output, err := run(exec.Command("kubectl", "apply", "-f", tmpFile))
	if err != nil {
		Fail(fmt.Sprintf("failed to deploy eBPF profiler: %s, output: %s", err, output))
	}

	By("waiting for eBPF profiler to be ready")
	Eventually(func(g Gomega) {
		output, err := run(exec.Command(
			"kubectl", "rollout", "status", "daemonset/ebpf-profiler",
			"--namespace", operatorNs,
			"--timeout=10s",
		))
		g.Expect(err).NotTo(HaveOccurred(), "eBPF profiler rollout status: %s", output)
	}, 120*time.Second, 5*time.Second).Should(Succeed())
}

func teardownEbpfProfiler(operatorNs string) {
	By("tearing down eBPF profiler")
	_, _ = run(exec.Command(
		"kubectl", "delete", "daemonset", "ebpf-profiler",
		"--namespace", operatorNs,
		"--ignore-not-found",
	))
	_, _ = run(exec.Command(
		"kubectl", "delete", "configmap", "ebpf-profiler-config",
		"--namespace", operatorNs,
		"--ignore-not-found",
	))
}

func verifyProfiles(
	g Gomega,
	timestampLowerBound time.Time,
	checkResourceAttributes bool,
) {
	askTelemetryMatcherForMatchingProfiles(
		g,
		shared.ExpectAtLeastOne,
		timestampLowerBound,
		checkResourceAttributes,
	)
}

func askTelemetryMatcherForMatchingProfiles(
	g Gomega,
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	checkResourceAttributes bool,
) {
	requestUrl := compileTelemetryMatcherUrlForProfiles(
		expectationMode,
		timestampLowerBound,
		checkResourceAttributes,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForProfiles(
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	checkResourceAttributes bool,
) string {
	baseUrl := fmt.Sprintf("%s/matching-profiles", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixNano(), 10))
	params.Add(shared.QueryParamCheckResourceAttributes, strconv.FormatBool(checkResourceAttributes))
	params.Add(shared.QueryParamOperatorNamespace, operatorNamespace)
	return baseUrl + "?" + params.Encode()
}
