// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type ResolvedInstrumentationDelivery string

const (
	ResolvedInstrumentationDeliveryImageVolume   = ResolvedInstrumentationDelivery(dash0v1alpha1.InstrumentationDeliveryImageVolume)
	ResolvedInstrumentationDeliveryInitContainer = ResolvedInstrumentationDelivery(dash0v1alpha1.InstrumentationDeliveryInitContainer)
)

// ResolveInstrumentationDelivery converts the operator configuration's spec.instrumentWorkloads.instrumentationDelivery
// value into the final decision which delivery mechanism is used by the workload modifier), taking the detected
// Kubernetes version into account.
//
// If delivery is "image-volume" and the Kubernetes version is older than 1.31, the function logs a warning and
// returns init container anyway. If the Kubernetes version could not be detected, the function trusts the explicit
// choice. For "auto" without a detected version, the function falls back to init container, since image volumes need a
// sufficiently recent Kubernetes version.
func ResolveInstrumentationDelivery(
	delivery string,
	versionInfo KubernetesVersionInfo,
	versionDetected bool,
	logWarningForEmptyValue bool,
	logger logd.Logger,
) ResolvedInstrumentationDelivery {
	// maintenance note: this switch statement needs to be updated once we move to the default being "auto"
	switch delivery {

	case string(dash0v1alpha1.InstrumentationDeliveryImageVolume):
		if !versionDetected {
			// no K8s version detected, use the requested delivery mechanism without further checks
			return ResolvedInstrumentationDeliveryImageVolume
		}
		if versionInfo.Major > imageVolumesAutoMinimumMajorVersion {
			// K8s version >= 2.x, image-volume has been requested, use image volume
			return ResolvedInstrumentationDeliveryImageVolume
		}
		if versionInfo.Major == imageVolumesAutoMinimumMajorVersion && versionInfo.Minor >= imageVolumesAlwaysMinimumMinorVersion {
			// K8s version >= 1.31, image-volume has been requested, use image volume
			return ResolvedInstrumentationDeliveryImageVolume
		}
		// K8s version < 1.31, reject using image volume
		logger.WarnTelemetryCollectionIssue(fmt.Sprintf(
			"spec.instrumentWorkloads.instrumentationDelivery is set to %q, but the Kubernetes version is %s, "+
				"which does not support image volumes. The setting will be ignored, the operator will use the init "+
				"container approach for instrumenting workloads.",
			ResolvedInstrumentationDeliveryImageVolume, versionInfo.VersionString,
		))
		return ResolvedInstrumentationDeliveryInitContainer

	case string(dash0v1alpha1.InstrumentationDeliveryAuto):
		if !versionDetected {
			// no K8s version detected, and no explicit delivery mechanism requested, fall back to init container
			logger.WarnTelemetryCollectionIssue(
				"spec.instrumentWorkloads.instrumentationDelivery is set to \"auto\", but the operator has not been able to " +
					"detect the Kubernetes version. Falling back to the init container approach for instrumenting workloads.",
			)
			return ResolvedInstrumentationDeliveryInitContainer
		}
		if versionInfo.Major > imageVolumesAutoMinimumMajorVersion {
			// K8s version >= 2.x, use image volume
			return ResolvedInstrumentationDeliveryImageVolume
		}
		if versionInfo.Major == imageVolumesAutoMinimumMajorVersion && versionInfo.Minor >= imageVolumesAutoMinimumMinorVersion {
			// K8s version >= 1.36, use image volume
			return ResolvedInstrumentationDeliveryImageVolume
		}
		// K8s version < 1.36, use init container
		return ResolvedInstrumentationDeliveryInitContainer

	case string(dash0v1alpha1.InstrumentationDeliveryInitContainer):
		// init container has been requested explicitly, no further checks necessary
		return ResolvedInstrumentationDeliveryInitContainer

	default:
		if delivery != "" || logWarningForEmptyValue {
			logger.WarnTelemetryCollectionIssue(fmt.Sprintf(
				"unknown instrumentation delivery: \"%s\". Falling back to the init container approach for instrumenting workloads.",
				delivery,
			))
		} else {
			logger.Debug(fmt.Sprintf(
				"unknown instrumentation delivery: \"%s\". Falling back to the init container approach for instrumenting workloads.",
				delivery,
			))
		}
		return ResolvedInstrumentationDeliveryInitContainer
	}
}
