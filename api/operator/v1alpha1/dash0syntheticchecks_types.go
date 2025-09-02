// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Dash0SyntheticCheckSpec defines the desired state of Dash0SyntheticCheck
type Dash0SyntheticCheckSpec struct {
	// +kubebuilder:validation:Required
	Display Dash0SyntheticCheckDisplay `json:"display"`

	// +kubebuilder:validation:Required
	Plugin Dash0SyntheticCheckPlugin `json:"plugin"`

	// +kubebuilder:validation:Required
	Schedule Dash0SyntheticCheckSchedule `json:"schedule"`

	// +kubebuilder:validation:Required
	Retries Dash0SyntheticCheckRetries `json:"retries"`

	// +kubebuilder:validation:Required
	Notifications Dash0SyntheticCheckNotifications `json:"notifications"`

	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
}

// Dash0SyntheticCheckDisplay defines the display configuration
type Dash0SyntheticCheckDisplay struct {
	// Short-form name for the view to be shown prominently within the view list and atop
	// the screen when the view is selected.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// Dash0SyntheticCheckPlugin defines the plugin configuration
// +kubebuilder:validation:XValidation:rule="self.kind == 'http'"
type Dash0SyntheticCheckPlugin struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=http
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	Spec Dash0SyntheticCheckHTTPPluginSpec `json:"spec"`
}

// Dash0SyntheticCheckHTTPPluginSpec defines the HTTP plugin specification
type Dash0SyntheticCheckHTTPPluginSpec struct {
	// +kubebuilder:validation:Required
	Request Dash0SyntheticCheckHTTPRequest `json:"request"`

	// +kubebuilder:validation:Required
	Assertions Dash0SyntheticCheckHTTPAssertions `json:"assertions"`
}

// Dash0SyntheticCheckHTTPRequest defines the HTTP request configuration
type Dash0SyntheticCheckHTTPRequest struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=get;post;put;delete;head;patch
	Method string `json:"method"`

	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=follow;disabled
	Redirects string `json:"redirects"`

	// +kubebuilder:validation:Required
	TLS Dash0SyntheticCheckHTTPTLS `json:"tls"`

	// +kubebuilder:validation:Required
	Tracing Dash0SyntheticCheckHTTPTracing `json:"tracing"`

	// +kubebuilder:validation:Required
	Headers []Dash0SyntheticCheckHTTPHeader `json:"headers"`

	// +kubebuilder:validation:Required
	QueryParameters []Dash0SyntheticCheckHTTPQueryParameter `json:"queryParameters"`

	// +kubebuilder:validation:Optional
	BasicAuthentication *Dash0SyntheticCheckHTTPBasicAuthentication `json:"basicAuthentication,omitempty"`

	// +kubebuilder:validation:Optional
	Body *Dash0SyntheticCheckHTTPBody `json:"body,omitempty"`
}

// Dash0SyntheticCheckHTTPTLS defines TLS configuration
type Dash0SyntheticCheckHTTPTLS struct {
	// +kubebuilder:validation:Required
	AllowInsecure bool `json:"allowInsecure"`
}

// Dash0SyntheticCheckHTTPTracing defines tracing configuration
type Dash0SyntheticCheckHTTPTracing struct {
	// +kubebuilder:validation:Required
	AddTracingHeaders bool `json:"addTracingHeaders"`
}

// Dash0SyntheticCheckHTTPHeader defines an HTTP header
type Dash0SyntheticCheckHTTPHeader struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// Dash0SyntheticCheckHTTPQueryParameter defines a query parameter
type Dash0SyntheticCheckHTTPQueryParameter struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// Dash0SyntheticCheckHTTPBasicAuthentication defines basic authentication
type Dash0SyntheticCheckHTTPBasicAuthentication struct {
	// +kubebuilder:validation:Required
	Username string `json:"username"`

	// +kubebuilder:validation:Required
	Password string `json:"password"`
}

// Dash0SyntheticCheckHTTPBody defines HTTP request body
type Dash0SyntheticCheckHTTPBody struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=json;graphql;form;raw
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	Spec Dash0SyntheticCheckHTTPBodySpec `json:"spec"`
}

// Dash0SyntheticCheckHTTPBodySpec defines the body specification
type Dash0SyntheticCheckHTTPBodySpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=2048
	Content string `json:"content"`
}

// Dash0SyntheticCheckHTTPAssertions defines HTTP assertions
type Dash0SyntheticCheckHTTPAssertions struct {
	// +kubebuilder:validation:Required
	CriticalAssertions []Dash0SyntheticCheckAssertion `json:"criticalAssertions"`

	// +kubebuilder:validation:Required
	DegradedAssertions []Dash0SyntheticCheckAssertion `json:"degradedAssertions"`
}

// Dash0SyntheticCheckAssertion defines a check assertion
// +kubebuilder:validation:XValidation:rule="(self.kind == 'status_code' && has(self.spec.operator) && has(self.spec.value)) || (self.kind == 'response_header' && has(self.spec.key) && has(self.spec.operator) && has(self.spec.value)) || (self.kind == 'json_body' && has(self.spec.jsonPath) && has(self.spec.operator) && has(self.spec.value)) || (self.kind == 'text_body' && has(self.spec.operator) && has(self.spec.value)) || (self.kind == 'timing' && has(self.spec.type) && has(self.spec.operator) && has(self.spec.value)) || (self.kind == 'error' && has(self.spec.value)) || (self.kind == 'ssl_certificate' && has(self.spec.value))"
type Dash0SyntheticCheckAssertion struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=status_code;response_header;json_body;text_body;timing;error;ssl_certificate
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	Spec Dash0SyntheticCheckAssertionSpec `json:"spec"`
}

// Dash0SyntheticCheckAssertionSpec defines assertion specification
type Dash0SyntheticCheckAssertionSpec struct {
	// For status_code, timing assertions
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=is;is_not;gt;lt;gte;lte
	Operator *string `json:"operator,omitempty"`

	// For all assertions
	// +kubebuilder:validation:Optional
	Value *string `json:"value,omitempty"`

	// For response_header assertions
	// +kubebuilder:validation:Optional
	Key *string `json:"key,omitempty"`

	// For json_body assertions
	// +kubebuilder:validation:Optional
	JSONPath *string `json:"jsonPath,omitempty"`

	// For timing assertions
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=total;dns;connection;ssl;request;response
	Type *string `json:"type,omitempty"`
}

// Dash0SyntheticCheckSchedule defines the schedule configuration
type Dash0SyntheticCheckSchedule struct {
	// Whether to run checks against all specified locations for every run, or just a single location per run.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=all_locations;random_location
	Strategy string `json:"strategy"`

	// Specifies a check evaluation frequency.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`(\d+(ms|s|m|h|d|w|M|Q|y))+`
	Interval string `json:"interval"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=128
	Locations []string `json:"locations"`
}

// Dash0SyntheticCheckRetries defines the retry configuration
// +kubebuilder:validation:XValidation:rule="(self.kind == 'off') || (self.kind == 'fixed' && has(self.spec.attempts) && has(self.spec.delay)) || (self.kind == 'linear' && has(self.spec.attempts) && has(self.spec.delay) && has(self.spec.maximumDelay)) || (self.kind == 'exponential' && has(self.spec.attempts) && has(self.spec.delay) && has(self.spec.maximumDelay))"
type Dash0SyntheticCheckRetries struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=off;fixed;linear;exponential
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	Spec Dash0SyntheticCheckRetriesSpec `json:"spec"`
}

// Dash0SyntheticCheckRetriesSpec defines retry specification
type Dash0SyntheticCheckRetriesSpec struct {
	// For fixed, linear, exponential retries
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Attempts *int `json:"attempts,omitempty"`

	// For fixed, linear, exponential retries
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`(\d+(ms|s|m|h|d|w|M|Q|y))+`
	Delay *string `json:"delay,omitempty"`

	// For linear, exponential retries
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`(\d+(ms|s|m|h|d|w|M|Q|y))+`
	MaximumDelay *string `json:"maximumDelay,omitempty"`
}

// Dash0SyntheticCheckNotifications defines notification configuration
type Dash0SyntheticCheckNotifications struct {
	// The channel IDs that notifications should be sent to in case the check is critical or degraded after
	// executing all the retries.
	// +kubebuilder:validation:Required
	Channels []NotificationChannelID `json:"channels"`
}

// NotificationChannelID is the unique identifier for a notification channel.
// +kubebuilder:validation:Format="uuid"
type NotificationChannelID string

// Dash0SyntheticCheckStatus defines the observed state of Dash0SyntheticCheck
type Dash0SyntheticCheckStatus struct {
}

// Dash0SyntheticCheck is the Schema for the dash0syntheticchecks API
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +groupName=operator.dash0.com
type Dash0SyntheticCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0SyntheticCheckSpec   `json:"spec,omitempty"`
	Status Dash0SyntheticCheckStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// Dash0SyntheticCheckList contains a list of Dash0SyntheticCheck
type Dash0SyntheticCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0SyntheticCheck `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0SyntheticCheck{}, &Dash0SyntheticCheckList{})
}
