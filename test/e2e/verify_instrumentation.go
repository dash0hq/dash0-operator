// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

const (
	verifyTelemetryTimeout         = 90 * time.Second
	verifyTelemetryPollingInterval = 500 * time.Millisecond
)

func verifyThatWorkloadHasBeenInstrumented(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	images Images,
	instrumentationBy string,
) string {
	By(fmt.Sprintf("%s: waiting for the workload to get instrumented (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		verifyLabels(
			g,
			namespace,
			workloadType,
			true,
			images,
			instrumentationBy,
		)
		verifySuccessfulInstrumentationEvent(g, namespace, workloadType, instrumentationBy)
	}, 20*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

	By(fmt.Sprintf("%s: waiting for spans to be captured", workloadType))
	var testId string
	if isBatch {
		testIdTimeoutSeconds := 30
		if workloadType == "cronjob" {
			// Cronjob pods are only scheduled once a minute, so we might need to wait a while for a job to be started
			// and for the ID to become available, hence increasing the timeout for the surrounding "Eventually".
			testIdTimeoutSeconds = 100
		}
		By(fmt.Sprintf("waiting for the test ID file to be written by the %s under test", workloadType))
		Eventually(func(g Gomega) {
			// For resource types like batch jobs/cron jobs, the application under test generates the test ID and writes it
			// to a volume that maps to a host path. We read the test ID from the host path and use it to verify the spans.
			testIdBytes, err := os.ReadFile(fmt.Sprintf("test-resources/e2e-test-volumes/test-uuid/%s.test.id", workloadType))
			g.Expect(err).NotTo(HaveOccurred())
			testId = string(testIdBytes)
			By(fmt.Sprintf("%s: test ID file is available (test ID: %s)", workloadType, testId))

		}, time.Duration(testIdTimeoutSeconds)*time.Second, 200*time.Millisecond).Should(Succeed())
	} else {
		// For resource types that are available as a service (daemonset, deployment etc.) we send HTTP requests with
		// a unique ID as a query parameter. When checking the produced spans that the OTel collector writes to disk via
		// the file exporter, we can verify that the span is actually from the currently running test case by inspecting
		// the http.target span attribute. This guarantees that we do not accidentally pass the test due to a span from
		// a previous test case.
		testIdUuid := uuid.New()
		testId = testIdUuid.String()
		By(fmt.Sprintf("%s: test ID: %s", workloadType, testId))
	}

	httpPathWithQuery := fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId)
	Eventually(func(g Gomega) {
		verifySpans(g, isBatch, workloadType, port, httpPathWithQuery)
	}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
	By(fmt.Sprintf("%s: matching spans have been received", workloadType))
	return testId
}

func verifyThatInstrumentationHasBeenReverted(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		workloadType,
		port,
		isBatch,
		testId,
		instrumentationBy,
		false,
	)
}

func verifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		workloadType,
		port,
		isBatch,
		testId,
		instrumentationBy,
		true,
	)
}

func verifyThatInstrumentationIsRevertedEventually(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
	expectOptOutLabel bool,
) {
	By(fmt.Sprintf(
		"%s: waiting for the instrumentation to get removed from the workload (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		if expectOptOutLabel {
			verifyOnlyOptOutLabelIsPresent(g, namespace, workloadType)
		} else {
			verifyNoDash0Labels(g, namespace, workloadType)
		}
		verifySuccessfulUninstrumentationEvent(g, namespace, workloadType, instrumentationBy)
	}, 30*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

	// Add some buffer time between the workloads being restarted and verifying that no spans are produced/captured.
	time.Sleep(10 * time.Second)

	secondsToCheckForSpans := 20
	if workloadType == "cronjob" {
		// Pod for cron jobs only get scheduled once a minute, since the cronjob schedule format does not allow for jobs
		// starting every second. Thus, to make the test valid, we need to monitor for spans a little bit longer than
		// for other workload types.
		secondsToCheckForSpans = 80
	}
	httpPathWithQuery := fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId)
	By(
		fmt.Sprintf("%s: verifying that spans are no longer being captured (checking for %d seconds)",
			workloadType,
			secondsToCheckForSpans,
		))
	Consistently(func(g Gomega) {
		verifyNoSpans(isBatch, workloadType, port, httpPathWithQuery)
	}, time.Duration(secondsToCheckForSpans)*time.Second, 1*time.Second).Should(Succeed())

	By(fmt.Sprintf("%s: matching spans are no longer captured", workloadType))
}
