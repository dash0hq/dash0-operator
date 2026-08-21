// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// ResolveServiceTrafficDistribution returns the value for spec.trafficDistribution on the services the operator
// manages, or nil when the field must be omitted because the Kubernetes version does not enable it by default.
//
// The value is always "PreferClose", never "PreferSameZone". Upstream marks PreferClose as deprecated in favour of
// PreferSameZone but documents it as the original name with exactly the same meaning (k8s.io/api, core/v1/types.go).
// PreferClose is accepted by every API server version that has the field at all, whereas PreferSameZone additionally
// requires the PreferSameTrafficDistribution feature gate, which is beta and can be switched off by the cluster
// administrator - so PreferClose is the only value that cannot be rejected.
//
// The field is omitted when the Kubernetes version could not be detected, mirroring how ResolveInstrumentationDelivery
// falls back conservatively for an unknown version. Setting a field the API server prunes would be worse than leaving
// zone-aware routing off: the desired object and the live object never converge and the operator would rewrite the
// service on every single reconcile. That is also why 1.30 is excluded even though the field exists there - its
// ServiceTrafficDistribution feature gate defaults to false. The version check cannot rule that loop out entirely,
// since an administrator can still disable the gate on 1.31 and 1.32, where it is beta rather than locked; it only
// avoids the case that would otherwise hit every cluster on those versions. The operator supports Kubernetes 1.25 and
// later.
func ResolveServiceTrafficDistribution(
	versionInfo KubernetesVersionInfo,
	logger logd.Logger,
) *string {
	preferClose := corev1.ServiceTrafficDistributionPreferClose
	if !versionInfo.Detected {
		logger.Debug("the Kubernetes API server version could not be detected, omitting spec.trafficDistribution on " +
			"the operator's services; zone-aware routing is not available")
		return nil
	}
	if versionInfo.IsAtLeast(trafficDistributionMinimumVersion) {
		return &preferClose
	}
	logger.Debug(fmt.Sprintf(
		"the Kubernetes API server version is %s, which does not enable spec.trafficDistribution by default (requires "+
			"1.%d or later); zone-aware routing is not available",
		versionInfo, trafficDistributionMinimumVersion.Minor))
	return nil
}
