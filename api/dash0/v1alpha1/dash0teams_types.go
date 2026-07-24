// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0Team is the Schema for the Dash0Team API
//
// +kubebuilder:object:root=true
// +groupName=dash0.com
// +kubebuilder:subresource:status
type Dash0Team struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0TeamSpec   `json:"spec,omitempty"`
	Status Dash0TeamStatus `json:"status,omitempty"`
}

// Dash0TeamSpec defines the desired state of Dash0Team.
type Dash0TeamSpec struct {
	// Display configuration for the team. Carries the human-facing name shown in the Dash0 UI, an optional
	// description, and the color gradient used for the team's badge.
	// +kubebuilder:validation:Required
	Display Dash0TeamDisplay `json:"display"`

	// The declarative membership of the team. Each entry is either an internal member ID (the dash0.com/id
	// label value, e.g. "user_01ABC...") or an email address. Emails are matched case-insensitively against
	// the organization's members and translated to internal IDs on the server side during reconciliation.
	// Unresolvable emails cause the synchronization to fail with the offending emails reported on
	// status.synchronizationResults[].synchronizationError.
	//
	// An empty list means "no members"; omitting the field is not allowed.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:items:MinLength=1
	Members []string `json:"members"`
}

// Dash0TeamDisplay defines the human-facing attributes of a team.
type Dash0TeamDisplay struct {
	// The human-facing name of the team, as shown in the Dash0 UI. Distinct from the CR's metadata.name,
	// which is the technical name used by Kubernetes.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// An optional long-form description of the team, shown alongside the display name in the Dash0 UI.
	// +kubebuilder:validation:Optional
	Description string `json:"description,omitempty"`

	// The color gradient used for the team's badge in the Dash0 UI.
	// +kubebuilder:validation:Required
	Color Dash0TeamColor `json:"color"`
}

// Dash0TeamColor defines a color gradient from one color to another. Values are typically CSS-compatible color
// strings (for example, "#6366F1").
type Dash0TeamColor struct {
	// The start color of the gradient.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	From string `json:"from"`

	// The end color of the gradient.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	To string `json:"to"`
}

// Dash0TeamStatus defines the observed state of Dash0Team
type Dash0TeamStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus     `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                           `json:"synchronizedAt"`
	ValidationIssues       []string                                              `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0TeamSynchronizationResultPerEndpointAndDataset `json:"synchronizationResults"`
}

// Dash0TeamSynchronizationResultPerEndpointAndDataset defines the synchronization result per endpoint. Teams
// are organization-scoped and therefore do not carry a dataset segment; the type name mirrors the sibling
// notification channel status shape for consistency.
type Dash0TeamSynchronizationResultPerEndpointAndDataset struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is the HTTP status code that the Dash0 API returned for the failed synchronization attempt, if the
	// failure was caused by an unexpected HTTP response. It is 0 (absent) for successful synchronizations and for
	// transport-level errors (network errors, timeouts) where no HTTP response was received. It is used to decide
	// whether a failed synchronization should be retried.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

// Dash0TeamList contains a list of Dash0Team resources.
//
// +kubebuilder:object:root=true
type Dash0TeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0Team `json:"items"`
}
