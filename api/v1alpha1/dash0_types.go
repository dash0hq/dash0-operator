// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Dash0Spec defines the desired state of the Dash0 custom resource.
type Dash0Spec struct {
}

// Dash0Status defines the observed state of the Dash0 custom resource.
type Dash0Status struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Dash0 is the Schema for the dash0s API
type Dash0 struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0Spec   `json:"spec,omitempty"`
	Status Dash0Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// Dash0List contains a list of Dash0
type Dash0List struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0 `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0{}, &Dash0List{})
}
