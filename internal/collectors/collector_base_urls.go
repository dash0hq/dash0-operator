// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"fmt"

	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	oTelCollectorServiceBaseUrlPattern   = "http://%s-opentelemetry-collector-service.%s.svc.cluster.local:4318"
	oTelCollectorNodeLocalBaseUrlPattern = "http://$(%s):%d"
)

// RenderCollectorBaseUrls renders both possible base URLs for routing telemetry from instrumented workloads to the
// OpenTelemetry collector daemonset: the service URL of the collector DaemonSet and the node-local URL (node IP plus
// host port). The URL that is actually used for instrumentation is picked from these two values via
// SelectCollectorBaseUrl.
func RenderCollectorBaseUrls(oTelCollectorNamePrefix string, operatorNamespace string) util.PossibleCollectorUrls {
	return util.PossibleCollectorUrls{
		NodeLocalBaseUrl: fmt.Sprintf(
			oTelCollectorNodeLocalBaseUrlPattern,
			util.EnvVarDash0NodeIp,
			otelcolresources.OtlpHttpHostPort,
		),
		ServiceBaseUrl: fmt.Sprintf(
			oTelCollectorServiceBaseUrlPattern,
			oTelCollectorNamePrefix,
			operatorNamespace,
		),
	}
}

// SelectCollectorBaseUrl selects the collector base URL that is actually used for instrumenting workloads from the two
// rendered possibilities, depending on whether the service URL is forced and whether the cluster uses IPv6.
func SelectCollectorBaseUrl(
	possibleUrls util.PossibleCollectorUrls,
	forceOTelCollectorServiceUrl bool,
	isIPv6Cluster bool,
) string {
	if forceOTelCollectorServiceUrl {
		return possibleUrls.ServiceBaseUrl
	}

	// Using the node's IPv6 address for the collector base URL should actually just work:
	// if m.clusterInstrumentationConfig.IsIPv6Cluster {
	//	 oTelCollectorNodeLocalBaseUrlPattern = "http://[$(%s)]:%d"
	// }
	//
	// But apparently the Node.js OpenTelemetry SDK tries to resolve that as a hostname, resulting in
	// Error: getaddrinfo ENOTFOUND [2a05:d014:1bc2:3702:fc43:fec6:1d88:ace5]\n    at GetAddrInfoReqWrap.onlookup
	// all [as oncomplete] (node:dns:120:26)
	//
	// To avoid that, we fall back to the service URL of the collector in IPv6 clusters.
	//
	// Would be worth to give this another try after implementing
	// https://linear.app/dash0/issue/ENG-2132.
	if isIPv6Cluster {
		return possibleUrls.ServiceBaseUrl
	}

	// By default, and if forceOTelCollectorServiceUrl has not been set, and it is also not an IPv6 cluster, use a
	// node-local route by sending telemetry from workloads to the OTel collector daemonset pod on the same node via
	// the node's IP address and the collector's host port.
	return possibleUrls.NodeLocalBaseUrl
}
