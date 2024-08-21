// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

const (
	labelChangeTimeout     = 25 * time.Second
	verifyTelemetryTimeout = 40 * time.Second
	pollingInterval        = 500 * time.Millisecond
)

func verifyThatWorkloadHasBeenInstrumented(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	images Images,
	instrumentationBy string,
) {
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
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

	By(fmt.Sprintf("%s: waiting for spans to be captured", workloadType))

	// For workload types that are available as a service (daemonset, deployment etc.) we send HTTP requests with
	// a unique ID as a query parameter. When checking the produced spans, we can verify that the span is
	// actually from the currently running test case by inspecting the http.target span attribute. This guarantees
	// that we do not accidentally pass the test due to a span from a previous test case.
	//
	// For batch workloads (job, cronjob), which are not reachable via a service, the application will call itself via
	// HTTP instead, which will create spans as well.
	spanTimeout := verifyTelemetryTimeout
	if workloadType == "cronjob" {
		// Cronjob pods are only scheduled once a minute, so we might need to wait a while for a job to be started
		// and for spans to become available, hence increasing the timeout for "Eventually" block that waits for spans.
		spanTimeout = 90 * time.Second
	}
	httpPathWithQuery := fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId)
	Eventually(func(g Gomega) {
		verifySpans(g, isBatch, workloadType, port, httpPathWithQuery)
	}, spanTimeout, pollingInterval).Should(Succeed())
	By(fmt.Sprintf("%s: matching spans have been received", workloadType))
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
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

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

func waitForApplicationToBecomeReady(templateName string, waitCommand *exec.Cmd) error {
	By(fmt.Sprintf("waiting for %s to become ready", templateName))
	err := runAndIgnoreOutput(waitCommand)
	if err != nil {
		By(fmt.Sprintf("%s never became ready", templateName))
	} else {
		By(fmt.Sprintf("%s is ready now", templateName))
	}
	return err
}
