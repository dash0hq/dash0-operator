// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

var (
	defaultRetryBackoff = wait.Backoff{
		Steps:    5,
		Duration: 10 * time.Millisecond,
		Factor:   1.0,
		Jitter:   0.1,
	}
)

func Retry(operationLabel string, operation func() error, logger *logr.Logger) error {
	return RetryWithCustomBackoff(operationLabel, operation, defaultRetryBackoff, true, logger)
}

func RetryWithCustomBackoff(
	operationLabel string,
	operation func() error,
	backoff wait.Backoff,
	logAttempts bool,
	logger *logr.Logger,
) error {
	attempt := 0
	return retry.OnError(
		backoff,
		func(err error) bool {
			attempt += 1
			if attempt < backoff.Steps {
				if logAttempts {
					logger.Error(err,
						fmt.Sprintf(
							"%s failed in attempt %d/%d, will be retried.", operationLabel, attempt, backoff.Steps))
				}
			} else {
				logger.Error(err,
					fmt.Sprintf(
						"%s failed after attempt %d/%d, no more retries left.", operationLabel, attempt, backoff.Steps))
			}
			return true
		},
		operation,
	)
}
