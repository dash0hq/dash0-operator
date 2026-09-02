// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/util/wait"
	k8sretry "k8s.io/client-go/util/retry"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// Retry executes the given operation and retries with the provided backoff settings. If the optional logger
// logAttemptsTo is provided, individual failed attempts are logged at level info. If logAttemptsTo is nil, this
// function logs nothing. If the final attempt fails with an error or if a non-retryable error occurs, this function
// does neither log nor handle it. The final or non-retryable error is instead returned to the caller. It is expected
// that the caller then handles it appropriately (by logging it or otherwise). A backoff with Steps <= 0 is normalized
// to a single attempt, so that the operation is always executed at least once.
//
// Note: The documentation for the wait.Backoff struct when used with k8sretry.OnError is a bit confusing, because
// k8sretry.OnError apparently uses some fields differently then what the wait.Backoff struct documentation says.
// Here goes:
//
//	wait.Backoff{
//	  // The initial duration -- that is, the initial wait between retries.
//	  Duration: ...
//	  // Duration (the wait between retries) is multiplied by factor after each retry.
//	  Factor: ...
//	  // Additional random element for adding to the wait between retries.
//	  Jitter: ...
//
// ^ The parameters above are used as documented in wait.Backoff; but Steps and Cap are used differently. Basically,
//
//	  when wait.Backoff would stop increasing Duration due to either Steps or Cap, k8sretry.OnError instead uses this
//	  as a trigger to stop retrying, and it returns the last error to the client.
//
//	  // wait.Backoff effectively says that after Steps retries, the duration will no longer change due to `Factor`,
//	  // but remain constant.
//	  // However, according to the function godoc comment on wait.backoff.ExponentialBackoff, backoff.Steps is the
//	  // maximum number of retries. So when the number of unsuccessful retries is equal to "Steps", k8sretry.OnError
//	  // gives up and the most recent error is returned to the client.
//	  Steps: ...
//
//	  // wait.Backoff effectively says that Cap is another way to limit the increase of Duration between retries, in
//	  // addition to or as an alternative to Steps. When Duration hits Cap, it will no longer be incremented.
//	  // However, according to the function godoc comment on wait.backoff.ExponentialBackoff, k8sretry.OnError gives up
//	  // once "a sleep truncated by the cap on duration has been completed." That is, when the Duration hits Cap,
//	  // there is exactly one more retry, and if that is not successful, k8sretry.OnError gives up. Note that Cap is not
//	  // a limit to the aggregated duration of all retries, but a limit to the Duration between retries, when it is
//	  // increased via a Factor > 1.0.
//	  Cap: ...
//	}
func Retry(
	operationLabel string,
	operation func() error,
	backoff wait.Backoff,
	logAttemptsTo *logd.Logger,
) error {
	if backoff.Steps <= 0 {
		// k8sretry.OnError would not call the operation at all and return nil, that is, report success without ever
		// having executed the operation.
		backoff.Steps = 1
	}

	attempt := 0

	return k8sretry.OnError(
		backoff,

		// This function is called for every error, for the purpose of determining whether it is a retryable or non-retryable
		// error.
		func(err error) bool {
			if !IsRetryable(err) {
				return false
			}
			attempt += 1
			if attempt < backoff.Steps {
				if logAttemptsTo != nil {
					logAttemptsTo.Info(
						fmt.Sprintf(
							"%s failed in attempt %d/%d, will be retried: %v",
							operationLabel,
							attempt,
							backoff.Steps,
							err,
						))
				}
			}

			// Note: k8sretry.OnError stops retrying correctly after the specified number of Steps, no matter whether we
			// return true or false here. The bool we return here is only used if there are still retries left, then it is
			// used to decide whether the error can be retried or aborts the retrying before the final attempt due to a
			// non-retryable error.
			return true
		},

		// This is the action/operation that is being retried.
		operation,
	)
}

// IsRetryable reports whether the operation that produced err should be retried. Errors are retryable per default.
// To mark them as non-retryable, wrap them via NewRetryableError with retryable set to false.
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
