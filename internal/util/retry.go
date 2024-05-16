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

func Retry(operationLabel string, operation func() error, log *logr.Logger) error {
	backoff := defaultRetryBackoff
	attempt := 0
	return retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			attempt += 1
			if attempt < backoff.Steps {
				log.Error(err, fmt.Sprintf("%s failed in attempt %d/%d, will be retried.", operationLabel, attempt, backoff.Steps))
			} else {
				log.Error(err, fmt.Sprintf("%s failed after attempt %d/%d, no more retries left.", operationLabel, attempt, backoff.Steps))
			}
			return true
		},
		operation,
	)
}
