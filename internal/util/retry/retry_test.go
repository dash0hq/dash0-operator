// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package retry_test

import (
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const operationLabel = "test operation"

var (
	logger           logd.Logger
	capturingLogSink *CapturingLogSink
)

// backoffWithSteps returns a backoff with a negligible wait between attempts, so that the tests do not spend time
// sleeping.
func backoffWithSteps(steps int) wait.Backoff {
	return wait.Backoff{
		Duration: time.Microsecond,
		Factor:   1.0,
		Steps:    steps,
	}
}

var _ = Describe("Retry", func() {

	BeforeEach(func() {
		logger, capturingLogSink = NewCapturingLogger()
	})

	It("does not retry a successful operation", func() {
		attempts := 0
		err := retry.Retry(operationLabel, func() error {
			attempts++
			return nil
		}, backoffWithSteps(5), &logger)

		Expect(err).ToNot(HaveOccurred())
		Expect(attempts).To(Equal(1))
		capturingLogSink.HasNoLogMessages(Default)
	})

	It("stops retrying once the operation succeeds", func() {
		attempts := 0
		err := retry.Retry(operationLabel, func() error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("attempt %d failed", attempts)
			}
			return nil
		}, backoffWithSteps(5), &logger)

		Expect(err).ToNot(HaveOccurred())
		Expect(attempts).To(Equal(3))
	})

	It("returns the error of the final attempt after exhausting all steps", func() {
		attempts := 0
		err := retry.Retry(operationLabel, func() error {
			attempts++
			return fmt.Errorf("attempt %d failed", attempts)
		}, backoffWithSteps(3), &logger)

		Expect(err).To(MatchError("attempt 3 failed"))
		Expect(attempts).To(Equal(3))
	})

	It("aborts immediately when the first error is non-retryable", func() {
		attempts := 0
		nonRetryable := retry.NewRetryableError(errors.New("do not retry this"), false)
		err := retry.Retry(operationLabel, func() error {
			attempts++
			return nonRetryable
		}, backoffWithSteps(5), &logger)

		Expect(err).To(MatchError(nonRetryable))
		Expect(attempts).To(Equal(1))
		capturingLogSink.HasNoLogMessages(Default)
	})

	It("aborts as soon as a non-retryable error occurs, even with retries left", func() {
		attempts := 0
		nonRetryable := retry.NewRetryableError(errors.New("do not retry this"), false)
		err := retry.Retry(operationLabel, func() error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("attempt %d failed", attempts)
			}
			return nonRetryable
		}, backoffWithSteps(10), &logger)

		Expect(err).To(MatchError(nonRetryable))
		Expect(attempts).To(Equal(3))
	})

	Describe("normalizing the number of steps", func() {
		// Without normalization, k8sretry.OnError never executes the operation for a backoff with Steps <= 0 and
		// reports success, that is, it silently pretends the operation has succeeded.
		for _, steps := range []int{0, -1} {
			It(fmt.Sprintf("executes the operation once for a backoff with Steps=%d", steps), func() {
				attempts := 0
				err := retry.Retry(operationLabel, func() error {
					attempts++
					return errors.New("operation failed")
				}, backoffWithSteps(steps), &logger)

				Expect(err).To(MatchError("operation failed"))
				Expect(attempts).To(Equal(1))
			})
		}

		It("executes a successful operation once for a zero value backoff", func() {
			attempts := 0
			err := retry.Retry(operationLabel, func() error {
				attempts++
				return nil
			}, wait.Backoff{}, &logger)

			Expect(err).ToNot(HaveOccurred())
			Expect(attempts).To(Equal(1))
		})
	})

	It("executes the operation once for a backoff with a single step", func() {
		attempts := 0
		err := retry.Retry(operationLabel, func() error {
			attempts++
			return errors.New("operation failed")
		}, backoffWithSteps(1), &logger)

		Expect(err).To(MatchError("operation failed"))
		Expect(attempts).To(Equal(1))
	})

	Describe("logging attempts", func() {
		It("logs every failed attempt that will be retried, but not the final one", func() {
			err := retry.Retry(operationLabel, func() error {
				return errors.New("operation failed")
			}, backoffWithSteps(3), &logger)

			Expect(err).To(HaveOccurred())
			capturingLogSink.HasLogMessage(
				Default,
				"test operation failed in attempt 1/3, will be retried: operation failed",
			)
			capturingLogSink.HasLogMessage(
				Default,
				"test operation failed in attempt 2/3, will be retried: operation failed",
			)
			// The final attempt is not logged, the caller is expected to handle the returned error.
			capturingLogSink.HasNoLogMessage(
				Default,
				"test operation failed in attempt 3/3, will be retried: operation failed",
			)
		})

		It("retries and returns the final error without a logger", func() {
			attempts := 0
			err := retry.Retry(operationLabel, func() error {
				attempts++
				return fmt.Errorf("attempt %d failed", attempts)
			}, backoffWithSteps(3), nil)

			Expect(err).To(MatchError("attempt 3 failed"))
			Expect(attempts).To(Equal(3))
			capturingLogSink.HasNoLogMessages(Default)
		})
	})
})

var _ = Describe("IsRetryable", func() {

	It("reports a plain error as retryable", func() {
		Expect(retry.IsRetryable(errors.New("some error"))).To(BeTrue())
	})

	It("reports a RetryableError marked as retryable as retryable", func() {
		Expect(retry.IsRetryable(retry.NewRetryableError(errors.New("some error"), true))).To(BeTrue())
	})

	It("reports a RetryableError marked as non-retryable as non-retryable", func() {
		Expect(retry.IsRetryable(retry.NewRetryableError(errors.New("some error"), false))).To(BeFalse())
	})

	It("unwraps a non-retryable error", func() {
		wrapped := fmt.Errorf("outer: %w", retry.NewRetryableError(errors.New("inner"), false))
		Expect(retry.IsRetryable(wrapped)).To(BeFalse())
	})
})

var _ = Describe("RetryableError", func() {

	It("renders the message of the wrapped error", func() {
		Expect(retry.NewRetryableError(errors.New("some error"), false).Error()).To(Equal("some error"))
	})

	It("renders a placeholder message when no error is wrapped", func() {
		Expect(retry.NewRetryableError(nil, false).Error()).To(Equal("unknown retryable error"))
	})

	It("reports its retryable flag", func() {
		Expect(retry.NewRetryableError(errors.New("some error"), true).IsRetryable()).To(BeTrue())
		Expect(retry.NewRetryableError(errors.New("some error"), false).IsRetryable()).To(BeFalse())
	})
})
