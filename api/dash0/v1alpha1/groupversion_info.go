// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package v1alpha1 contains API Schema definitions for the dash0.com v1alpha1 API group. This group hosts
// Dash0 platform resources that the operator synchronizes to the Dash0 API — resources whose Kubernetes group
// intentionally matches the Dash0 platform group (dash0.com) rather than the operator-owned group
// (operator.dash0.com) used for operator plumbing types such as Dash0Monitoring.
// +kubebuilder:object:generate=true
// +groupName=dash0.com
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "dash0.com", Version: "v1alpha1"}

	// SchemeBuilder collects the functions that register this group-version's types into a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Dash0Team{}, &Dash0TeamList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
