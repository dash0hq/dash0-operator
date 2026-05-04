// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package v1beta1 contains API Schema definitions for the operator v1beta1 API group
// +kubebuilder:object:generate=true
// +groupName=operator.dash0.com
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "operator.dash0.com", Version: "v1beta1"}

	// SchemeBuilder collects the functions that register this group-version's types into a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Dash0Monitoring{}, &Dash0MonitoringList{},
		&Dash0NotificationChannel{}, &Dash0NotificationChannelList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
