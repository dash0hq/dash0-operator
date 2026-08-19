// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	k8sretry "k8s.io/client-go/util/retry"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

var (
	// The documentation for the wait.Backoff struct when used with k8sretry.OnError is a bit confusing, because
	// k8sretry.OnError apparently uses some fields differently then what the wait.Backoff struct documentation says.
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
	//   when wait.Backoff would stop increasing Duration due to either Steps or Cap, k8sretry.OnError instead uses this
	//   as a trigger to stop retrying, and it returns the last error to the client.
	//
	//   // wait.Backoff effectively says that after Steps retries, the duration will no longer change due to `Factor`,
	//   // but remain constant.
	//   // However, according to the function godoc comment on wait.backoff.ExponentialBackoff, backoff.Steps is the
	//   // maximum number of retries. So when the number of unsuccessful retries is equal to "Steps", k8sretry.OnError
	//   // gives up and the most recent error is returned to the client.
	//   Steps: ...
	//
	//   // wait.Backoff effectively says that Cap is another way to limit the increase of Duration between retries, in
	//   // addition to or as an alternative to Steps. When Duration hits Cap, it will no longer be incremented.
	//   // However, according to the function godoc comment on wait.backoff.ExponentialBackoff, k8sretry.OnError gives up
	//   // once "a sleep truncated by the cap on duration has been completed." That is, when the Duration hits Cap,
	//   // there is exactly one more retry, and if that is not successful, k8sretry.OnError gives up. Note that Cap is not
	//   // a limit to the aggregated duration of all retries, but a limit to the Duration between retries, when it is
	//   // increased via a Factor > 1.0.
	//   Cap: ...
	// }
	defaultRetryBackoff = wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   1.0,
		Steps:    5,
		Jitter:   0.1,
	}
)

// Retry executes the given operation and retries with the defaultRetryBackoff.
// If the final attempt fails with an error or if a non-retryable error occurs, the error is returned the caller.
// Individual attempts are logged, and if the final attempt fails, this is logged as an error as well.
func Retry(operationLabel string, operation func() error, logger logd.Logger) error {
	return RetryWithCustomBackoff(operationLabel, operation, defaultRetryBackoff, true, true, logger)
}

// RetryWithCustomBackoff executes the given operation and retries with the provided backoff settings.
func RetryWithCustomBackoff(
	operationLabel string,
	operation func() error,
	backoff wait.Backoff,
	logAttempts bool,
	logFinalFailureAsError bool,
	logger logd.Logger,
) error {
	attempt := 0
	return k8sretry.OnError(
		backoff,
		func(err error) bool {
			if !IsRetryable(err) {
				if logFinalFailureAsError {
					logger.Error(err,
						fmt.Sprintf(
							"%s failed with non-retryable error", operationLabel))
				}
				return false
			}

			attempt += 1
			if attempt < backoff.Steps {
				if logAttempts {
					logger.Info(
						fmt.Sprintf(
							"%s failed in attempt %d/%d, will be retried: %v",
							operationLabel,
							attempt,
							backoff.Steps,
							err,
						))
				}
			} else if logFinalFailureAsError {
				logger.Error(err,
					fmt.Sprintf(
						"%s failed after attempt %d/%d, no more retries left.", operationLabel, attempt, backoff.Steps))
			} else {
				logger.Info(fmt.Sprintf(
					"%s failed after attempt %d/%d, no more retries left, final error: %v.",
					operationLabel,
					attempt,
					backoff.Steps,
					err,
				))
			}

			// Note: k8sretry.OnError stops retrying correctly after the specified number of Steps, returning this
			// most recent error, no matter whether we return true or false. The bool return value is only used to
			// inspect errors and decide whether they even can be retried or not.
			return true
		},
		operation,
	)
}

// IsRetryable reports whether the operation that produced err should be retried. Errors are retryable per default,
// wrap them via NewRetryableError with retryable set to false to mark them as non-retryable.
func IsRetryable(err error) bool {
	var retryErr *RetryableError
	if errors.As(err, &retryErr) {
		return retryErr.retryable
	}
	return true
}

type RetryableError struct {
	err       error
	retryable bool
}

func NewRetryableError(err error, retryable bool) *RetryableError {
	return &RetryableError{err: err, retryable: retryable}
}

func (e *RetryableError) Error() string {
	if e == nil || e.err == nil {
		return "unknown retryable error"
	}
	return e.err.Error()
}

func (e *RetryableError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.retryable
}
