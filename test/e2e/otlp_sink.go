// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/http"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	otlpSinkChartPath   = "test/e2e/otlp-sink/helm-chart"
	otlpSinkReleaseName = "otlp-sink"

	otlpSinkNamespace = "otlp-sink"
)

var (
	telemetryMatcherBaseUrl    = "http://localhost:8080/telemetry-matcher"
	telemetryMatcherHttpClient *http.Client

	telemetryMatcherImage ImageSpec
)

func init() {
	// disable keep-alive
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	telemetryMatcherHttpClient = &http.Client{Transport: t}
}

func determineTelemetryMatcherImage() {
	repositoryPrefix := getEnvOrDefault("TEST_IMAGE_REPOSITORY_PREFIX", defaultImageRepositoryPrefix)
	imageTag := getEnvOrDefault("TEST_IMAGE_TAG", defaultImageTag)
	pullPolicy := getEnvOrDefault("TEST_IMAGE_PULL_POLICY", defaultPullPolicy)
	telemetryMatcherImage =
		determineContainerImage(
			"TELEMETRY_MATCHER",
			repositoryPrefix,
			"telemetry-matcher",
			imageTag,
			pullPolicy,
		)
}

func deployOtlpSink(cleanupSteps *neccessaryCleanupSteps) {
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

	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.repository=%s", telemetryMatcherImage.repository),
	)
	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.tag=%s", telemetryMatcherImage.tag),
	)
	helmArgs = append(
		helmArgs,
		"--set",
		fmt.Sprintf("telemetryMatcher.image.pullPolicy=%s", telemetryMatcherImage.pullPolicy),
	)
	cleanupSteps.removeOtlpSink = true
	Expect(runAndIgnoreOutput(exec.Command("helm", helmArgs...))).To(Succeed())
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
