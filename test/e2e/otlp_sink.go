// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	otlpSinkChartPath   = "test-resources/otlp-sink/helm-chart"
	otlpSinkReleaseName = "otlp-sink"

	otlpSinkNamespace = "otlp-sink"
)

func deployOtlpSink(workingDir string, cleanupSteps *neccessaryCleanupSteps) {
	createDirAndDeleteOldExportedTelemetry()

	helmArgs := []string{"install",
		"--namespace",
		otlpSinkNamespace,
		"--create-namespace",
		"--wait",
		"--timeout",
		"60s",
		otlpSinkReleaseName,
		otlpSinkChartPath,
	}

	if !isKindCluster() {
		e2eTestExportDir := fmt.Sprintf(
			"%s/test-resources/e2e/volumes/otlp-sink",
			workingDir,
		)
		helmArgs =
			append(helmArgs, "--set", fmt.Sprintf("collector.telemetryFileExportVolume.path=%s", e2eTestExportDir))
	}

	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
	cleanupSteps.removeOtlpSink = true
}

func createDirAndDeleteOldExportedTelemetry() {
	_ = os.MkdirAll("test-resources/e2e/volumes/otlp-sink", 0755)
	By("deleting old telemetry files")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/traces.jsonl")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/metrics.jsonl")
	_ = os.Remove("test-resources/e2e/volumes/otlp-sink/logs.jsonl")
	By("creating telemetry dump files")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/traces.jsonl")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/metrics.jsonl")
	_, _ = os.Create("test-resources/e2e/volumes/otlp-sink/logs.jsonl")
}

func uninstallOtlpSink(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removeOtlpSink {
		return
	}
	By("removing otlp-sink")
	Expect(runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			otlpSinkReleaseName,
			"--namespace",
			otlpSinkNamespace,
			"--ignore-not-found",
		))).To(Succeed())
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"ns",
			otlpSinkNamespace,
			"--wait",
			"--ignore-not-found",
		))).To(Succeed())
}
