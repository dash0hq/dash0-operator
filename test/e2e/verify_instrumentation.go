// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testEndpoint           = "/dash0-k8s-operator-test"
	labelChangeTimeout     = 25 * time.Second
	verifyTelemetryTimeout = 70 * time.Second
	pollingInterval        = 500 * time.Millisecond

	cronjob = "cronjob"
)

func verifyThatWorkloadHasBeenInstrumented(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	images Images,
	instrumentationBy string,
) {
	By(fmt.Sprintf("%s %s: waiting for the workload to get instrumented (polling its labels and events to check)",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		verifyLabels(
			g,
			namespace,
			runtime,
			workloadType,
			true,
			images,
			instrumentationBy,
		)
		verifySuccessfulInstrumentationEvent(g, namespace, runtime, workloadType, instrumentationBy)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

	// After instrumenting the workload, old pods (from the uninstrumented workload) can still be up and running,
	// especially if the new pods take too long to become ready. Before we start waiting for spans, we make sure that
	// the pods of the instrumented workload have become ready and the old uninstrumented pods have terminated.
	waitForRollout(namespace, runtime, workloadType)
	waitForApplicationToBecomeResponsive(
		runtime,
		workloadType,
		testEndpoint,
		"",
	)

	By(fmt.Sprintf("%s %s: waiting for spans to be captured", runtime.runtimeTypeLabel, workloadType.workloadTypeString))

	// For workload types that are available as a service (daemonset, deployment etc.) we send HTTP requests with
	// a unique ID as a query parameter. When checking the produced spans, we can verify that the span is
	// actually from the currently running test case by inspecting the http.target span attribute. This guarantees
	// that we do not accidentally pass the test due to a span from a previous test case.
	//
	// For batch workloads (job, cronjob), which are not reachable via a service, the application will call itself via
	// HTTP instead, which will create spans as well.
	spanTimeout := verifyTelemetryTimeout
	if workloadType.workloadTypeString == "cronjob" {
		// Cronjob pods are only scheduled once a minute, so we might need to wait a while for a job to be started
		// and for spans to become available, hence increasing the timeout for "Eventually" block that waits for spans.
		spanTimeout = 90 * time.Second
	}
	query := fmt.Sprintf("id=%s", testId)
	timestampLowerBound := time.Now()
	Eventually(func(g Gomega) {
		verifySpans(
			g,
			runtime,
			workloadType,
			testEndpoint,
			query,
			timestampLowerBound,
			true,
		)
	}, spanTimeout, pollingInterval).Should(Succeed())
	By(fmt.Sprintf("%s %s: matching spans have been received", runtime.runtimeTypeLabel, workloadType.workloadTypeString))
}

//nolint:unparam
func verifyThatInstrumentationHasBeenReverted(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		runtime,
		workloadType,
		testId,
		instrumentationBy,
		false,
	)
}

func verifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationIsRevertedEventually(
		namespace,
		runtime,
		workloadType,
		testId,
		instrumentationBy,
		true,
	)
}

func verifyThatInstrumentationIsRevertedEventually(
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	instrumentationBy string,
	expectOptOutLabel bool,
) {
	By(fmt.Sprintf(
		"%s %s: waiting for the instrumentation to get removed from the workload (polling its labels and events to check)",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString))
	Eventually(func(g Gomega) {
		if expectOptOutLabel {
			verifyOnlyOptOutLabelIsPresent(g, namespace, runtime, workloadType)
		} else {
			verifyNoDash0Labels(g, namespace, runtime, workloadType)
		}
		verifySuccessfulUninstrumentationEvent(g, namespace, runtime, workloadType, instrumentationBy)
	}, labelChangeTimeout, pollingInterval).Should(Succeed())

	// After uninstrumenting the workload, old pods (from the instrumented workload) can still be up and running,
	// especially if the new pods take too long to become ready. Before we can run the "verifying that spans are no
	// longer being captured" check in a meaningful way, we need to make sure that the pods of the uninstrumented
	// workload have become ready and the old pods have terminated.
	waitForRollout(namespace, runtime, workloadType)
	waitForApplicationToBecomeResponsive(
		runtime,
		workloadType,
		testEndpoint,
		"",
	)
	time.Sleep(5 * time.Second)

	secondsToCheckForSpans := 20
	if workloadType.workloadTypeString == cronjob {
		// Pod for cron jobs only get scheduled once a minute, since the cronjob schedule format does not allow for jobs
		// starting every second. Thus, to make the test valid, we need to monitor for spans a little bit longer than
		// for other workload types.
		secondsToCheckForSpans = 80
	}
	By(
		fmt.Sprintf("%s %s: verifying that spans are no longer being captured (checking for %d seconds)",
			runtime.runtimeTypeLabel,
			workloadType.workloadTypeString,
			secondsToCheckForSpans,
		))
	query := fmt.Sprintf("id=%s", testId)
	timestampLowerBound := time.Now()
	Consistently(func(g Gomega) {
		verifyNoSpans(g, runtime, workloadType, testEndpoint, query, timestampLowerBound)
	}, time.Duration(secondsToCheckForSpans)*time.Second, 1*time.Second).Should(Succeed())

	By(fmt.Sprintf(
		"%s %s: matching spans are no longer captured",
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
	))
}
