// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

// SignalControlComponents contains the optional settings for the managed Signal Control workloads.
type SignalControlComponents struct {
	// Settings for the Signal Control collector Deployment.
	//
	// +kubebuilder:validation:Optional
	Collector *SignalControlCollectorSettings `json:"collector,omitempty"`

	// Settings for the Edge Proxy Deployment.
	//
	// +kubebuilder:validation:Optional
	EdgeProxy *EdgeProxySettings `json:"edgeProxy,omitempty"`
}

// SignalControlCollectorSettings are the settings for the Signal Control collector Deployment.
type SignalControlCollectorSettings struct {
	// The number of replicas of the Signal Control collector Deployment (Helm value
	// operator.collectors.signalControlCollectorReplicas, default 2).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`
}

// EdgeProxySettings are the settings for the Edge Proxy Deployment.
type EdgeProxySettings struct {
	// The number of Edge Proxy pods to run (Helm value operator.signalControl.edgeProxy.replicas, default 2).
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Whether to enable pprof for the Edge Proxy pods (Helm value operator.signalControl.edgeProxy.enablePprof, default
	// false). Only enable this if instructed by Dash0 support.
	//
	// +kubebuilder:validation:Optional
	EnablePprof *bool `json:"enablePprof,omitempty"`
}
