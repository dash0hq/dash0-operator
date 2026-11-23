// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// DatasetFinalizerId is the finalizer that keeps a Dash0Dataset resource until its dataset has been deleted in Dash0.
const DatasetFinalizerId = "operator.dash0.com/dash0-dataset-finalizer"

// Dash0Dataset is the Schema for the Dash0Dataset API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Dash0Dataset struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0DatasetSpec   `json:"spec,omitempty"`
	Status Dash0DatasetStatus `json:"status,omitempty"`
}

// Dash0DatasetSpec defines the desired state of Dash0Dataset. Every field except slug and display is optional. A field
// that is omitted takes the server default when the dataset is created, and keeps its stored value when the dataset is
// updated.
type Dash0DatasetSpec struct {
	// The identifier that telemetry and queries use for this dataset. It cannot be changed after creation.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="slug is immutable"
	Slug string `json:"slug"`

	// Display configuration for the dataset.
	// +kubebuilder:validation:Required
	Display Dash0DatasetDisplay `json:"display"`

	// Whether this dataset is the organization's preferred dataset, which the Dash0 UI opens by default. At most one
	// dataset per organization is preferred; setting it clears the flag on the previous one.
	// +kubebuilder:validation:Optional
	Preferred *bool `json:"preferred,omitempty"`

	// Who may do what with this dataset. The list replaces the stored grants; an omitted or empty list keeps them
	// unchanged.
	// +kubebuilder:validation:Optional
	Permissions []Dash0DatasetPermission `json:"permissions,omitempty"`

	// What Dash0 derives, stores and redacts for this dataset.
	// +kubebuilder:validation:Optional
	Privacy *Dash0DatasetPrivacy `json:"privacy,omitempty"`

	// Transformations Dash0 applies to ingested telemetry.
	// +kubebuilder:validation:Optional
	Processing *Dash0DatasetProcessing `json:"processing,omitempty"`

	// How long each signal is kept.
	// +kubebuilder:validation:Optional
	Retention *Dash0DatasetRetention `json:"retention,omitempty"`
}

// Dash0DatasetDisplay defines the human-facing attributes of a dataset.
type Dash0DatasetDisplay struct {
	// The human-facing name of the dataset, as shown in the Dash0 UI.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Dash0DatasetPermission grants actions on the dataset to exactly one user, team or role.
//
// +kubebuilder:validation:XValidation:rule="(has(self.userId) ? 1 : 0) + (has(self.teamId) ? 1 : 0) + (has(self.role) ? 1 : 0) == 1",message="exactly one of userId, teamId or role must be set"
type Dash0DatasetPermission struct {
	// The actions granted.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Actions []Dash0DatasetAction `json:"actions"`

	// The role the grant applies to. The only role that can be granted is basic_member.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=basic_member
	Role *string `json:"role,omitempty"`

	// The ID of the team the grant applies to.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinLength=1
	TeamId *string `json:"teamId,omitempty"`

	// The ID of the user the grant applies to.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinLength=1
	UserId *string `json:"userId,omitempty"`
}

// Dash0DatasetAction is an action that can be granted on a dataset.
//
// +kubebuilder:validation:Enum="dataset:read";"dataset:write";"dataset:delete";"dataset:createCheckRule";"dataset:createRecordingRuleGroup";"dataset:createSLO";"dataset:createSyntheticCheck";"dataset:createAgent0Automation"
type Dash0DatasetAction string

// Dash0DatasetPrivacy defines what Dash0 derives, stores and redacts for a dataset.
type Dash0DatasetPrivacy struct {
	// +kubebuilder:validation:Optional
	GeoLocation *Dash0DatasetGeoLocationPrivacy `json:"geoLocation,omitempty"`

	// +kubebuilder:validation:Optional
	IpAddresses *Dash0DatasetIpAddressPrivacy `json:"ipAddresses,omitempty"`

	// +kubebuilder:validation:Optional
	Redaction *Dash0DatasetRedaction `json:"redaction,omitempty"`
}

// Dash0DatasetGeoLocationPrivacy defines how geolocation derived from end-user IP addresses is handled.
type Dash0DatasetGeoLocationPrivacy struct {
	// +kubebuilder:validation:Optional
	EndUser *Dash0DatasetGeoLocationStorage `json:"endUser,omitempty"`
}

// Dash0DatasetGeoLocationStorage defines how much geolocation derived from end-user IP addresses is stored.
type Dash0DatasetGeoLocationStorage struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=do_not_store;up_to_city;up_to_country
	Storage *string `json:"storage,omitempty"`
}

// Dash0DatasetIpAddressPrivacy defines how end-user IP addresses are handled.
type Dash0DatasetIpAddressPrivacy struct {
	// +kubebuilder:validation:Optional
	EndUser *Dash0DatasetIpAddressStorage `json:"endUser,omitempty"`
}

// Dash0DatasetIpAddressStorage defines how end-user IP addresses derived from the X-FORWARDED-FOR and X-REAL-IP HTTP
// headers are stored.
type Dash0DatasetIpAddressStorage struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=anonymize;do_not_store
	Storage *string `json:"storage,omitempty"`
}

// Dash0DatasetRedaction defines which sensitive data Dash0 redacts before it persists a signal.
type Dash0DatasetRedaction struct {
	// Detection and redaction of payment card numbers and related sensitive data.
	// +kubebuilder:validation:Optional
	CardholderData *Dash0DatasetCardholderDataRedaction `json:"cardholderData,omitempty"`

	// Redaction policies for GenAI attributes.
	// +kubebuilder:validation:Optional
	GenAi *Dash0DatasetGenAiRedaction `json:"genAi,omitempty"`
}

// Dash0DatasetCardholderDataRedaction enables or disables cardholder data redaction.
type Dash0DatasetCardholderDataRedaction struct {
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
}

// Dash0DatasetGenAiRedaction defines the redaction policies for GenAI attributes. Conversation content, tool-call
// input/output and tool-call error messages are controlled independently.
type Dash0DatasetGenAiRedaction struct {
	// Redaction of gen_ai.input.messages, gen_ai.output.messages and gen_ai.system_instructions.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=disabled;redact
	Conversation *string `json:"conversation,omitempty"`

	// Redaction of tool-call error messages.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=disabled;redact
	Errors *string `json:"errors,omitempty"`

	// Redaction of gen_ai.tool.call.arguments and gen_ai.tool.call.result.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=disabled;redact
	ToolCalls *string `json:"toolCalls,omitempty"`
}

// Dash0DatasetProcessing defines the transformations Dash0 applies to ingested telemetry.
type Dash0DatasetProcessing struct {
	// +kubebuilder:validation:Optional
	Darkplane *Dash0DatasetDarkplane `json:"darkplane,omitempty"`

	// Whether semantic convention upgrades are executed: disabled, latest, or a version vX.Y.Z up to which (inclusive)
	// upgrades are executed.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^(disabled|latest|v[0-9]+\.[0-9]+\.[0-9]+)$`
	SemanticConventionUpgrades *string `json:"semanticConventionUpgrades,omitempty"`

	// Convert span events to log records correlated with the parent span.
	// +kubebuilder:validation:Optional
	SpanEventsToLogs *bool `json:"spanEventsToLogs,omitempty"`
}

// Dash0DatasetDarkplane defines Darkplane-specific opt-ins for a dataset.
type Dash0DatasetDarkplane struct {
	// Darkplane processing of OpenTelemetry telemetry natively emitted by coding agents.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=disabled;enabled
	RespectNativeCodingAgentTelemetry *string `json:"respectNativeCodingAgentTelemetry,omitempty"`
}

// Dash0DatasetRetention defines the total retention per signal.
type Dash0DatasetRetention struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="30d";"60d";"90d";"120d";"210d";"390d"
	Logs *string `json:"logs,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="30d";"60d";"90d";"120d";"210d";"390d"
	Traces *string `json:"traces,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="30d";"60d";"90d";"120d";"210d";"390d"
	WebEvents *string `json:"webEvents,omitempty"`

	// Metrics retention is not selectable and is always 30 days.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="30d"
	Metrics *string `json:"metrics,omitempty"`

	// Profile retention is not selectable and is always 30 days.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum="30d"
	Profiles *string `json:"profiles,omitempty"`
}

// Dash0DatasetStatus defines the observed state of Dash0Dataset
type Dash0DatasetStatus struct {
	SynchronizationStatus  dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt         metav1.Time                                       `json:"synchronizedAt"`
	ValidationIssues       []string                                          `json:"validationIssues,omitempty"`
	SynchronizationResults []Dash0DatasetSynchronizationResultPerEndpoint    `json:"synchronizationResults"`
}

// Dash0DatasetSynchronizationResultPerEndpoint defines the synchronization result per endpoint.
type Dash0DatasetSynchronizationResultPerEndpoint struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	Dash0ApiEndpoint      string                                            `json:"dash0ApiEndpoint,omitempty"`
	// The slug of the dataset in Dash0.
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin          string `json:"dash0Origin,omitempty"`
	SynchronizationError string `json:"synchronizationError,omitempty"`
	// HttpStatusCode is the HTTP status code that the Dash0 API returned for the failed synchronization attempt, if the
	// failure was caused by an unexpected HTTP response. It is 0 (absent) for successful synchronizations and for
	// transport-level errors. It is used to decide whether a failed synchronization should be retried.
	// +kubebuilder:validation:Optional
	HttpStatusCode int `json:"httpStatusCode,omitempty"`
}

// Dash0DatasetList contains a list of Dash0Dataset resources.
//
// +kubebuilder:object:root=true
type Dash0DatasetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0Dataset `json:"items"`
}
