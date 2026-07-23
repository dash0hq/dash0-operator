// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package v1 contains API Schema definitions for the openslo v1 API group.
//
// Unlike the operator's first-party CRDs (which live under the operator.dash0.com group), the SLO CRD adopts the
// upstream OpenSLO v1 SLO shape (kind "SLO", spec matching the OpenSLO v1 SLO specification) so users can apply
// OpenSLO manifests to the cluster.
//
// Kubernetes constraint / provisional decision (TODO(phase3): confirm with Michele): the plan calls for the bare
// OpenSLO group "openslo" (apiVersion "openslo/v1"). However, the Kubernetes API server rejects any
// CustomResourceDefinition whose spec.group is not a DNS subdomain with at least one dot ("openslo" is invalid), so a
// bare-group CRD cannot be installed in any real cluster. We therefore use the domain-qualified group "openslo.com"
// for the Kubernetes CRD (Kubernetes apiVersion "openslo.com/v1"). The Dash0 SLO API document version matches this
// group: the controller sends the same "openslo.com/v1" apiVersion and "SLO" kind in the Dash0 API body (see the slo
// controller), so the CR and the API body share one apiVersion with no version mapping.
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
	// GroupVersion is the group version used to register these objects. The Kubernetes CRD group is domain-qualified
	// ("openslo.com") because Kubernetes requires CRD groups to contain a dot; the Dash0 API body uses the same
	// "openslo.com/v1" document version (set explicitly by the controller).
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
