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
	// The documentation for the wait.Backoff struct when used with retry.OnError is a bit confusing, because
	// retry.OnError apparently uses some fields differently then what the wait.Backoff struct documentation says.
	// Here goes:
	//
	// wait.Backoff{
	//   // The initial duration -- that is, the initial wait between retries.
	//   Duration: ...
	//   // Duration (the wait between retries) is multiplied by factor after each retry.
	//   Factor: ...
	//   // Additional random element for adding to the wait between retries.
	//   Jitter: ...
	//
	// ^ The parameters above are used as documented in wait.Backoff; but Steps and Cap are used differently. Basically,
	//   when wait.Backoff would stop increasing Duration due to either Steps or Cap, retry.OnError instead uses this as
	//   a trigger to stop retrying, and it returns the last error to the client.
	//
	//   // wait.Backoff effectively says that after Steps retries, the duration will no longer change but remain
	//   // However, according to the function godoc comment on wait.backoff.ExponentialBackoff, backoff.Steps is the
	//   // maximum number of retries. So when the number of unsuccessful retries is equal to "Steps", the retry.OnError
	//   // gives up and the most recent error is returned to the client.
	//   Steps: ...
	//
	//   // wait.Backoff effectively says that Cap is another way to limit the increase of Duration between retries, in
	//   // addition to or as an alternative to Steps. When Duration hits Cap, it will no longer be incremented.
	//   // With retry.OnError retrying is not stopped when the total time has reached Cap (as one might think), but
	//   // instead it works like this: If factor would increase Duration to a value higher than Cap, then retry.OnError
	//   // will stop retrying and return the last error.
	//   Cap: ...
	// }
	defaultRetryBackoff = wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   1.0,
		Steps:    5,
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
				// Note: retry.OnError stops retrying correctly after the specified number of Steps, returning this
				// most recent error, no matter whether we return true or false. The bool return value is only used to
				// inspect errors and decide whether to they even can be retried or not.
			}
			return true
		},
		operation,
	)
}
