// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr/funcr"

	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The zone coverage reporter", func() {
	ctx := context.Background()

	var logged []string
	var recordingLogger logd.Logger
	var msgs ZoneCoverageMessages

	BeforeEach(func() {
		logged = nil
		recordingLogger = logd.NewLogger(funcr.New(func(_ string, args string) {
			logged = append(logged, args)
		}, funcr.Options{}))
		msgs = ZoneCoverageMessages{
			Warn: func(zoneCount int, replicaCount int32) string {
				return fmt.Sprintf("insufficient: %d zones, %d replicas", zoneCount, replicaCount)
			},
			Resolved: func(zoneCount int, replicaCount int32) string {
				return fmt.Sprintf("resolved: %d zones, %d replicas", zoneCount, replicaCount)
			},
			ListErrDebug: "cannot list nodes",
		}
	})

	Describe("evaluating a zone/replica combination", func() {
		It("warns once per distinct zone/replica combination, not on every check", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)

			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring("insufficient: 3 zones, 2 replicas"))

			// A steady state must not produce a warning again.
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(1))

			// A changed zone count is a new situation and is reported again.
			reporter.report(4, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(2))
		})

		It("does not warn when there are at least as many replicas as zones", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)
			reporter.report(3, 3, checkedAt, msgs, recordingLogger)
			reporter.report(3, 5, checkedAt, msgs, recordingLogger)
			Expect(logged).To(BeEmpty())
		})

		It("does not warn on clusters with no zone labels or a single zone", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)
			reporter.report(0, 1, checkedAt, msgs, recordingLogger)
			reporter.report(1, 1, checkedAt, msgs, recordingLogger)
			Expect(logged).To(BeEmpty())
		})

		It("logs the warning again when a replica change leaves the issue unresolved", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)
			reporter.report(3, 1, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring("insufficient: 3 zones, 1 replicas"))

			// Raising the replicas but not far enough is still insufficient: warn again.
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(2))
			Expect(logged[1]).To(ContainSubstring("insufficient: 3 zones, 2 replicas"))
		})

		It("logs the resolution when a replica change resolves an active warning, and warns again if it reoccurs", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring("insufficient"))

			// Raising the replica count to match the zones resolves the issue and is announced once.
			reporter.report(3, 3, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(2))
			Expect(logged[1]).To(ContainSubstring("resolved: 3 zones, 3 replicas"))

			// Lowering the replicas again is a new insufficient situation: warn once more.
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(3))
			Expect(logged[2]).To(ContainSubstring("insufficient"))
		})

		It("announces the resolution when the issue resolves on its own without a replica change", func() {
			reporter := &ZoneCoverageReporter{}
			checkedAt := time.Unix(0, 0)
			reporter.report(3, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(1))
			Expect(logged[0]).To(ContainSubstring("insufficient"))

			// The zone count drops on its own; the resolution is announced.
			reporter.report(2, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(2))
			Expect(logged[1]).To(ContainSubstring("resolved: 2 zones, 2 replicas"))

			// Once resolved, a further healthy evaluation is not announced again.
			reporter.report(2, 2, checkedAt, msgs, recordingLogger)
			Expect(logged).To(HaveLen(2))
		})
	})

	Describe("without a node metadata client", func() {
		It("is a no-op", func() {
			reporter := NewZoneCoverageReporter(nil)
			reporter.Report(ctx, 1, msgs, recordingLogger)
			Expect(logged).To(BeEmpty())
		})

		It("is a no-op on a nil reporter", func() {
			var reporter *ZoneCoverageReporter
			Expect(func() { reporter.Report(ctx, 1, msgs, recordingLogger) }).ToNot(Panic())
			Expect(logged).To(BeEmpty())
		})
	})
})
