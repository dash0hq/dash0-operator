// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// ResolveServiceTrafficDistribution returns the value for spec.trafficDistribution on the services the operator
// manages, or nil when the field must be omitted because the Kubernetes version does not know it.
//
// The value is always "PreferClose", never "PreferSameZone". Upstream marks PreferClose as deprecated in favour of
// PreferSameZone but documents it as the original name with exactly the same meaning (k8s.io/api, core/v1/types.go).
// PreferClose is accepted by every API server version that has the field at all, whereas PreferSameZone additionally
// requires the PreferSameTrafficDistribution feature gate, which is beta and can be switched off by the cluster
// administrator - so PreferClose is the only value that cannot be rejected.
//
// The field is omitted when the Kubernetes version could not be detected, mirroring how ResolveInstrumentationDelivery
// falls back conservatively for an unknown version. Setting a field the API server does not know would be worse than
// leaving zone-aware routing off: the API server strips it, so the desired object and the live object never converge
// and the operator would rewrite the service on every single reconcile. The operator supports Kubernetes 1.25 and
// later, so versions without this field are well within the supported range.
func ResolveServiceTrafficDistribution(
	versionInfo KubernetesVersionInfo,
	versionDetected bool,
	logger logd.Logger,
) *string {
	preferClose := corev1.ServiceTrafficDistributionPreferClose
	if !versionDetected {
		logger.Debug("the Kubernetes version could not be detected, omitting spec.trafficDistribution on the " +
			"operator's services; zone-aware routing is not available")
		return nil
	}
	if versionInfo.Major > trafficDistributionMinimumMajorVersion {
		return &preferClose
	}
	if versionInfo.Major == trafficDistributionMinimumMajorVersion &&
		versionInfo.Minor >= trafficDistributionMinimumMinorVersion {
		return &preferClose
	}
	logger.Debug(fmt.Sprintf(
		"the Kubernetes version is %s, which does not support spec.trafficDistribution (added in 1.%d); zone-aware "+
			"routing is not available", versionInfo.VersionString, trafficDistributionMinimumMinorVersion))
	return nil
}
