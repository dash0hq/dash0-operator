// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package v1 contains the API schema for the openslo.com/v1 SLO CRD, so that OpenSLO documents can be applied to the
// cluster.
//
// The group is "openslo.com" and not the bare "openslo" that OpenSLO documents use, because the API server rejects a
// CRD group without a dot. The Dash0 API accepts both and canonicalizes to "openslo.com/v1", so the custom resource
// and the API body share one apiVersion.
//
// TODO(phase3): confirm the group choice with Michele.
//
// +kubebuilder:object:generate=true
// +groupName=openslo.com
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "openslo.com", Version: "v1"}

	// SchemeBuilder collects the functions that register this group-version's types into a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&SLO{}, &SLOList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
