// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
)

type testConfig interface {
	GetWorkloadType() workloadType
	GetRuntimeType() runtimeType
}

func runInParallel[C testConfig](
	testConfigs []C,
	testStep func(C),
) {
	var passedMutex sync.Mutex
	passed := make(map[string]bool)
	var wg sync.WaitGroup
	for _, config := range testConfigs {
		workloadTypeString := config.GetWorkloadType().workloadTypeString
		runtime := config.GetRuntimeType()
		mapKey := fmt.Sprintf("%s-%s", runtime.runtimeTypeLabel, workloadTypeString)
		passed[mapKey] = false
		wg.Add(1)
		go func(cfg C) {
			defer GinkgoRecover()
			defer wg.Done()
			e2ePrint("(before test step: %s %s)\n", runtime.runtimeTypeLabel, workloadTypeString)
			testStep(cfg)
			e2ePrint("(after test step: %s %s)\n", runtime.runtimeTypeLabel, workloadTypeString)
			passedMutex.Lock()
			passed[mapKey] = true
			passedMutex.Unlock()
		}(config)
	}
	wg.Wait()

	// Fail early if one of the workloads has not passed the test step. Because of runInParallel and
	// the business with the (required) "defer GinkgoRecover()", Ginkgo needs a little help with that. Without this
	// additional check, a failure occurring in testStep might not make the test fail immediately, but is only reported
	// after the whole test has finished. This might lead to some slightly weird and hard-to-understand behavior,
	// because it looks like the has passed testStep, and then the whole test fails with something that should have been
	// reported much earlier.
	for _, config := range testConfigs {
		mapKey := fmt.Sprintf("%s-%s", config.GetRuntimeType().runtimeTypeLabel, config.GetWorkloadType().workloadTypeString)
		if !passed[mapKey] {
			Fail(
				fmt.Sprintf(
					"test config %s %s has not passed a test step executed in parallel",
					config.GetRuntimeType().runtimeTypeLabel,
					config.GetWorkloadType().workloadTypeString,
				))
		}
	}
}
