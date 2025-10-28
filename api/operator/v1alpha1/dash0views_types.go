// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

// Dash0View is the Schema for the Dash0View API
//
// +kubebuilder:object:root=true
// +groupName=operator.dash0.com
// +kubebuilder:subresource:status
type Dash0View struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Dash0ViewSpec   `json:"spec,omitempty"`
	Status Dash0ViewStatus `json:"status,omitempty"`
}

// Dash0ViewSpec defines the desired state of Dash0View
type Dash0ViewSpec struct {
	// The view type describes where this view configuration is intended to be applied in the UI.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=resources;spans;logs;metrics;failed_checks;web_events
	Type string `json:"type"`

	// +kubebuilder:validation:Required
	Display Dash0ViewDisplay `json:"display"`

	// +kubebuilder:validation:Optional
	Permissions []Dash0ViewPermission `json:"permissions,omitempty"`

	// A set of attribute keys to group the view by. This property may be missing to indicate that
	// no configuration was historically made for this view, i.e., the view was saved before the
	// grouping capability was added. In that case, you can assume a default grouping should be used.
	// +kubebuilder:validation:Optional
	GroupBy []string `json:"groupBy,omitempty"`

	// The filter for the whole view.
	// +kubebuilder:validation:Optional
	Filter []Dash0ViewFilter `json:"filter,omitempty"`

	// An implicit filter the user cannot change that will always be applied to the view. It will not
	// be shown in the UI.
	// +kubebuilder:validation:Optional
	ImplicitFilter []Dash0ViewFilter `json:"implicitFilter,omitempty"`

	// +kubebuilder:validation:Optional
	Table *Dash0ViewTable `json:"table,omitempty"`

	// A visualization configuration for the view. We only support a single visualization for now per view.
	// +kubebuilder:validation:Optional
	Visualizations []Dash0ViewVisualization `json:"visualizations,omitempty"`
}

// Dash0ViewDisplay defines the display configuration
type Dash0ViewDisplay struct {
	// Short-form name for the view to be shown prominently within the view list and atop
	// the screen when the view is selected.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Long-form explanation what the view is doing. Shown within the list of views to help
	// team members understand the purpose of the view.
	// +kubebuilder:validation:Optional
	Description string `json:"description,omitempty"`

	// The name of the folder in which the view is located. For example, if
	// the view is located in the root folder, this field will be an empty array.
	//
	// A view can only be located within a single folder. However, it can be nested.
	// Meaning, a value such as `["Shop", "Checkout"]` describes that the view is
	// residing within a folder called `Checkout` and that folder is within a folder called
	// `Shop`.
	// +kubebuilder:validation:Optional
	Folder []string `json:"folder,omitempty"`
}

// Dash0ViewPermission defines permission settings for a view
type Dash0ViewPermission struct {
	// +kubebuilder:validation:Optional
	TeamId string `json:"teamId,omitempty"`

	// +kubebuilder:validation:Optional
	UserId string `json:"userId,omitempty"`

	// Use role identifiers such as `admin` and `basic_member` to reference user groups.
	// +kubebuilder:validation:Optional
	Role string `json:"role,omitempty"`

	// Outlines possible actions that matching views can take with this view.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum="views:read";"views:write";"views:delete"
	Actions []string `json:"actions"`
}

// Dash0ViewFilter defines a filter condition
type Dash0ViewFilter struct {
	// The attribute key to be filtered.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// The match operation for filtering attributes.
	// +kubebuilder:validation:Required
	Operator Dash0ViewFilterOperator `json:"operator"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Value *AnyValue `json:"value,omitempty"`

	// List of values to match against. This parameter is mandatory for the is_one_of and is_not_one_of operators.
	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Values []AnyValue `json:"values,omitempty"`
}

// Dash0ViewFilterOperator defines the operator for a filter in a view.
// +kubebuilder:validation:Enum=is;is_not;is_set;is_not_set;is_one_of;is_not_one_of;gt;lt;gte;lte;matches;does_not_match;contains;does_not_contain;starts_with;does_not_start_with;ends_with;does_not_end_with;is_any
type Dash0ViewFilterOperator string

// AnyValue represents a value that can be a string or a structured object
// +kubebuilder:pruning:PreserveUnknownFields
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type AnyValue struct {
	// +kubebuilder:validation:Optional
	StringValue *string `json:"stringValue,omitempty"`

	// +kubebuilder:validation:Optional
	BoolValue *bool `json:"boolValue,omitempty"`

	// +kubebuilder:validation:Optional
	IntValue *string `json:"intValue,omitempty"`

	// +kubebuilder:validation:Optional
	DoubleValue *string `json:"doubleValue,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	ArrayValue *ArrayValue `json:"arrayValue,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	KvlistValue *KvlistValue `json:"kvlistValue,omitempty"`

	// +kubebuilder:validation:Optional
	BytesValue *string `json:"bytesValue,omitempty"`
}

// ArrayValue represents an array of AnyValue messages
// +kubebuilder:pruning:PreserveUnknownFields
type ArrayValue struct {
	// Array of values. The array may be empty (contain 0 elements).
	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	Values []json.RawMessage `json:"values"`
}

// KvlistValue represents a list of KeyValue messages
// +kubebuilder:pruning:PreserveUnknownFields
type KvlistValue struct {
	// A collection of key/value pairs of key-value pairs. The list may be empty (may contain 0 elements).
	// The keys MUST be unique (it is not allowed to have more than one value with the same key).
	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	Values []KeyValue `json:"values"`
}

// KeyValue represents a key-value pair that is used to store Span attributes, Link attributes, etc.
// +kubebuilder:pruning:PreserveUnknownFields
type KeyValue struct {
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	Value json.RawMessage `json:"value"`
}

// Dash0ViewTable defines table configuration
type Dash0ViewTable struct {
	// +kubebuilder:validation:Required
	Columns []Dash0ViewTableColumn `json:"columns"`

	// Any attribute keys to order by.
	// +kubebuilder:validation:Required
	Sort []Dash0ViewTableSort `json:"sort"`
}

// Dash0ViewTableColumn defines a table column
type Dash0ViewTableColumn struct {
	// The key of the attribute to be displayed in this column. This can also be a built-in column name.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// +kubebuilder:validation:Optional
	Label string `json:"label,omitempty"`

	// A CSS grid layout sizing instruction. Supports hard-coded sizing such as `7rem`, but also `min-content`
	// and similar variants.
	// +kubebuilder:validation:Optional
	ColSize string `json:"colSize,omitempty"`
}

// Dash0ViewTableSort defines sort configuration
type Dash0ViewTableSort struct {
	// Any attribute key to order by.
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ascending;descending
	// +kubebuilder:default=ascending
	Direction string `json:"direction"`
}

// Dash0ViewVisualization defines visualization configuration
type Dash0ViewVisualization struct {
	// The Y axis scale for the visualization.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=linear;log10
	YAxisScale string `json:"yAxisScale,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=logs_total;logs_rate;spans_total;spans_rate;spans_errors_total;spans_errors_rate;spans_errors_percentage;spans_duration_p99;spans_duration_p95;spans_duration_p90;spans_duration_p75;spans_duration_p50;spans_duration_avg
	// +kubebuilder:deprecated
	Metric string `json:"metric,omitempty"`

	// The renderer type for the visualization. The pattern matching is inspired by glob, where `*` can be used as a wildcard.
	// The matching prioritizes precise matches before wildcard matches.
	// If there are multiple renderers, select buttons will be added to allow toggling between the renderers.
	// +kubebuilder:validation:Optional
	// +kubebuilder:deprecated
	Renderer Dash0ViewRenderer `json:"renderer,omitempty"`

	// A set of visualizations (charts) to render as part of this view. We currently only expect
	// to render a single
	// +kubebuilder:validation:Required
	Renderers []Dash0ViewRenderer `json:"renderers"`
}

// Dash0ViewRenderer defines a visualization renderer.
// +kubebuilder:validation:Enum=traces-explorer/*/outliers;traces-explorer/*/red;logging/*/stacked_bar;resources/*/table-tree;resources/overview/overview;resources/services/red;resources/operations/red;resources/names/red;resources/k8s-cron-jobs/executions;resources/k8s-cron-jobs/red;resources/k8s-daemon-sets/scheduled-nodes;resources/k8s-daemon-sets/red;resources/k8s-deployments/replicas;resources/k8s-deployments/red;resources/k8s-jobs/executions;resources/k8s-jobs/red;resources/k8s-namespaces/red;resources/k8s-nodes/cpu-memory-disk;resources/k8s-nodes/status;resources/k8s-pods/cpu-memory;resources/k8s-pods/status;resources/k8s-pods/red;resources/k8s-replica-sets/replicas;resources/k8s-replica-sets/red;resources/k8s-stateful-sets/replicas;resources/k8s-stateful-sets/red;resources/faas/red
type Dash0ViewRenderer string

// Dash0ViewStatus defines the observed state of Dash0View
type Dash0ViewStatus struct {
	SynchronizationStatus dash0common.Dash0ApiResourceSynchronizationStatus `json:"synchronizationStatus"`
	SynchronizedAt        metav1.Time                                       `json:"synchronizedAt"`
	// +kubebuilder:validation:Optional
	Dash0Id string `json:"dash0Id,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Origin string `json:"dash0Origin,omitempty"`
	// +kubebuilder:validation:Optional
	Dash0Dataset         string   `json:"dash0Dataset,omitempty"`
	SynchronizationError string   `json:"synchronizationError,omitempty"`
	ValidationIssues     []string `json:"validationIssues,omitempty"`
}

//+kubebuilder:object:root=true

// Dash0ViewList contains a list of Dash0View resources.
type Dash0ViewList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dash0View `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dash0View{}, &Dash0ViewList{})
}
