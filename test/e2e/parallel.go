// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
)

type runInParallelConfig interface {
	GetMapKey() string
	GetLabel() string
}

func runInParallel[C runInParallelConfig](
	runInParallelConfigs []C,
	testStep func(C),
) {
	var passedMutex sync.Mutex
	passed := make(map[string]bool)
	var wg sync.WaitGroup
	for _, config := range runInParallelConfigs {
		mapKey := config.GetMapKey()
		passed[mapKey] = false
		wg.Add(1)
		go func(cfg C) {
			defer GinkgoRecover()
			defer wg.Done()
			e2ePrint("(before test step: %s)\n", config.GetLabel())
			testStep(cfg)
			e2ePrint("(after test step: %s)\n", config.GetLabel())
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
	for _, config := range runInParallelConfigs {
		if !passed[config.GetMapKey()] {
			Fail(
				fmt.Sprintf(
					"test config %s has not passed a test step executed in parallel",
					config.GetLabel(),
				))
		}
	}
}
