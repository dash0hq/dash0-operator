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
// value into the actual delivery mechanism used by the workload modifier, taking the detected Kubernetes versions into
// account.
//
// Image volumes need support from the kubelet, so the decision depends on the minimum kubelet version among the
// cluster's nodes (see MinimumKubeletVersionDetector) as well as on the Kubernetes API server version.
//
// If delivery is "image-volume", the function honors the explicit choice unless one of the two versions is known to be
// older than 1.31; in that case it logs a warning and returns init container. For "auto", the function returns image
// volume only if both the Kubernetes API server version and the minimum kubelet version are known to be at least 1.36;
// in particular it returns init container while the minimum kubelet version detection is still running.
func ResolveInstrumentationDelivery(
	delivery dash0v1alpha1.InstrumentationDelivery,
	apiServerVersionInfo KubernetesVersionInfo,
	minimumKubeletVersionInfo KubernetesVersionInfo,
	logWarningForEmptyValue bool,
	logger logd.Logger,
) ResolvedInstrumentationDelivery {
	// maintenance note: this switch statement needs to be updated once we move to the default being "auto"
	switch delivery {

	case dash0v1alpha1.InstrumentationDeliveryImageVolume:
		// Versions that have not been detected (yet) do not veto the explicit choice, only versions that are known to
		// be too old do.
		if apiServerVersionInfo.Detected &&
			!apiServerVersionInfo.IsAtLeast(imageVolumesHardMinimumVersion) {
			logger.WarnTelemetryCollectionIssue(fmt.Sprintf(
				"spec.instrumentWorkloads.instrumentationDelivery is set to %q, but the Kubernetes API server version is %s, "+
					"which does not support image volumes. The setting will be ignored, the operator will use the "+
					"init container approach for instrumenting workloads.",
				ResolvedInstrumentationDeliveryImageVolume, apiServerVersionInfo,
			))
			return ResolvedInstrumentationDeliveryInitContainer
		}
		if minimumKubeletVersionInfo.Detected &&
			!minimumKubeletVersionInfo.IsAtLeast(imageVolumesHardMinimumVersion) {
			logger.WarnTelemetryCollectionIssue(fmt.Sprintf(
				"spec.instrumentWorkloads.instrumentationDelivery is set to %q, but the cluster has nodes with "+
					"kubelet version %s, which does not support image volumes. The setting will be ignored, the "+
					"operator will use the init container approach for instrumenting workloads.",
				ResolvedInstrumentationDeliveryImageVolume, minimumKubeletVersionInfo,
			))
			return ResolvedInstrumentationDeliveryInitContainer
		}
		return ResolvedInstrumentationDeliveryImageVolume

	case dash0v1alpha1.InstrumentationDeliveryAuto:
		if !apiServerVersionInfo.Detected {
			// no K8s version detected, and no explicit delivery mechanism requested, fall back to init container
			logger.WarnTelemetryCollectionIssue(
				"spec.instrumentWorkloads.instrumentationDelivery is set to \"auto\", but the operator has not been able to " +
					"detect the Kubernetes API server version. Falling back to the init container approach for instrumenting " +
					"workloads.",
			)
			return ResolvedInstrumentationDeliveryInitContainer
		}
		if !minimumKubeletVersionInfo.Detected {
			// The nodes have not been inspected (successfully) yet, we cannot rule out that some of them are too old
			// for image volumes.
			logger.Debug("spec.instrumentWorkloads.instrumentationDelivery is set to \"auto\", but the minimum " +
				"kubelet version among the cluster's nodes is not known (yet). Using the init container approach " +
				"for instrumenting workloads until all nodes have been inspected for their kubelet version.")
			return ResolvedInstrumentationDeliveryInitContainer
		}
		if apiServerVersionInfo.IsAtLeast(imageVolumesAutoMinimumVersion) &&
			minimumKubeletVersionInfo.IsAtLeast(imageVolumesAutoMinimumVersion) {
			// API server and all kubelets are on version >= 1.36, use image volume
			return ResolvedInstrumentationDeliveryImageVolume
		}
		// API server or at least one kubelet is on version < 1.36, use init container
		return ResolvedInstrumentationDeliveryInitContainer

	case dash0v1alpha1.InstrumentationDeliveryInitContainer:
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
