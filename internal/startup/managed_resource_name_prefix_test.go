// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"strings"

	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// maxServiceNameLength is the maximum length of a Kubernetes service name (a DNS-1035 label).
const maxServiceNameLength = 63

// The Helm chart validates the name prefix for the resources the operator creates (OTEL_COLLECTOR_NAME_PREFIX and
// OTEL_TARGET_ALLOCATOR_NAME_PREFIX) against the longest service name of the enabled features, see the helper
// "dash0-operator.managedResourceNamePrefix" in helm-chart/dash0-operator/templates/_helpers.tpl. These tests pin the
// limits used there against the service names the operator actually creates. If one of them fails, update the limit
// in the helper, the corresponding Helm chart unit tests and the documentation of operator.managedResourceNamePrefix
// in helm-chart/dash0-operator/values.yaml.
var _ = Describe("the name prefix limits of the Helm chart", func() {
	DescribeTable("match the length of the longest service name",
		func(serviceName func(string) string, maxPrefixLengthInHelmChart int) {
			serviceNameSuffixLength := len(serviceName(""))
			Expect(maxServiceNameLength - serviceNameSuffixLength).To(Equal(maxPrefixLengthInHelmChart))

			longestPrefix := strings.Repeat("a", maxPrefixLengthInHelmChart)
			Expect(len(serviceName(longestPrefix))).To(Equal(maxServiceNameLength))
		},
		Entry("OpenTelemetry collector service (telemetry collection)",
			otelcolresources.ServiceName, 31),
		Entry("Signal Control collector service (operator.signalControl.enabled)",
			otelcolresources.SignalControlCollectorServiceName, 30),
		Entry("target allocator service (operator.prometheusCrdSupportEnabled)",
			taresources.ServiceName, 24),
	)
})
