// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package v1alpha1 contains API Schema definitions for the operator v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=operator.dash0.com
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "operator.dash0.com", Version: "v1alpha1"}

	// SchemeBuilder collects the functions that register this group-version's types into a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Dash0OperatorConfiguration{}, &Dash0OperatorConfigurationList{},
		&Dash0Monitoring{}, &Dash0MonitoringList{},
		&Dash0IntelligentEdge{}, &Dash0IntelligentEdgeList{},
		&Dash0SamplingRule{}, &Dash0SamplingRuleList{},
		&Dash0SyntheticCheck{}, &Dash0SyntheticCheckList{},
		&Dash0View{}, &Dash0ViewList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
