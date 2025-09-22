// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

// This file contains common types used in multiple custom resource definitions (e.g. Dash0Monitoring and
// Dash0OperatorConfiguration), and potentially also in multiple versions of those.

type ConditionType string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"
)

// Export describes the observability backend to which telemetry data will be sent. This can either be Dash0 or another
// OTLP-compatible backend. You can also combine up to three exporters (i.e. Dash0 plus gRPC plus HTTP). This allows
// sending the same data to two or three targets simultaneously. At least one exporter has to be defined.
//
// +kubebuilder:validation:MinProperties=1
type Export struct {
	// The configuration of the Dash0 ingress endpoint to which telemetry data will be sent.
	//
	// +kubebuilder:validation:Optional
	Dash0 *Dash0Configuration `json:"dash0,omitempty"`

	// The settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver via HTTP.
	//
	// +kubebuilder:validation:Optional
	Http *HttpConfiguration `json:"http,omitempty"`

	// The settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver via gRPC.
	//
	// +kubebuilder:validation:Optional
	Grpc *GrpcConfiguration `json:"grpc,omitempty"`
}

// Dash0Configuration describes to which Dash0 ingress endpoint telemetry data will be sent.
type Dash0Configuration struct {
	// The URL of the Dash0 ingress endpoint to which telemetry data will be sent. This property is mandatory. The value
	// needs to be the OTLP/gRPC endpoint of your Dash0 organization. The correct OTLP/gRPC endpoint can be copied fom
	// https://app.dash0.com -> organization settings -> "Endpoints". The correct endpoint value will always start with
	// `ingress.` and end in `dash0.com:4317`.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// The name of the Dash0 dataset to which telemetry data will be sent. This property is optional. If omitted, the
	// dataset "default" will be used.
	//
	// +kubebuilder:default=default
	Dataset string `json:"dataset,omitempty"`

	// Mandatory authorization settings for sending data to Dash0.
	//
	// +kubebuilder:validation:Required
	Authorization Authorization `json:"authorization"`

	// The base URL of the Dash0 API to talk to. This is not where telemetry will be sent, but it is used for managing
	// dashboards, check rules, synthetic checks and views via the operator. This property is optional. The value
	// needs to be the API endpoint of your Dash0 organization. The correct API endpoint can be copied fom
	// https://app.dash0.com -> organization settings -> "Endpoints" -> "API". The correct endpoint value will always
	// start with "https://api." and end in ".dash0.com"
	//
	// +kubebuilder:validation:Optional
	ApiEndpoint string `json:"apiEndpoint,omitempty"`
}

// Authorization contains the authorization settings for Dash0.
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authorization struct {
	// The Dash0 authorization token. This property is optional, but either this property or the SecretRef property has
	// to be provided. If both are provided, the token will be used and SecretRef will be ignored. The authorization
	// token for your Dash0 organization can be copied from https://app.dash0.com -> organization settings ->
	// "Auth Tokens".
	//
	// +kubebuilder:validation:Optional
	Token *string `json:"token,omitempty"` // either token or secret ref, with token taking precedence

	// A reference to a Kubernetes secret containing the Dash0 authorization token. This property is optional, and is
	// ignored if the token property is set. The authorization token for your Dash0 organization can be copied from
	// https://app.dash0.com -> organization settings -> "Auth Tokens".
	//
	// +kubebuilder:validation:Optional
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

type SecretRef struct {
	// The name of the secret containing the Dash0 authorization token. Defaults to "dash0-authorization-secret".
	// +kubebuilder:default=dash0-authorization-secret
	Name string `json:"name"`

	// The key of the value which contains the Dash0 authorization token. Defaults to "token"
	// +kubebuilder:default=token
	Key string `json:"key"`
}

// HttpConfiguration describe the settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver
// via HTTP.
type HttpConfiguration struct {
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent. This property is mandatory.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each HTTP request, for example for authorization. This property is optional.
	//
	// +kubebuilder:validation:Optional
	Headers []Header `json:"headers,omitempty"`

	// The encoding of the OTLP data when sent via HTTP. Can be either proto or json, defaults to proto.
	//
	// +kubebuilder:default=proto
	Encoding OtlpEncoding `json:"encoding,omitempty"`

	// Whether verification of TLS certificates should be skipped. Ignored when the endpoint uses an insecure connection.
	//
	// +kubebuilder:validation:Optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// GrpcConfiguration descibe the settings for an exporter to send telemetry to an arbitrary OTLP-compatible receiver
// via gRPC.
type GrpcConfiguration struct {
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent. This property is mandatory.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each gRPC request, for example for authorization. This property is optional.
	//
	// +kubebuilder:validation:Optional
	Headers []Header `json:"headers,omitempty"`

	// Explicitly defines whether TLS is used. Per default, a secure connection is assumed, unless the endpoint starts with 'http://'.
	//
	// +kubebuilder:validation:Optional
	Insecure *bool `json:"insecure,omitempty"`

	// Whether to skip verifying the server's certificate chain. It is a validation error to explicitly set
	// `insecure=true` and `insecureSkipVerify=true` at the same time, since `insecure` means TLS won't be used at all.
	//
	// +kubebuilder:validation:Optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// OtlpEncoding describes the encoding of the OTLP data when sent via HTTP.
//
// +kubebuilder:validation:Enum=proto;json
type OtlpEncoding string

const (
	Proto OtlpEncoding = "proto"
	Json  OtlpEncoding = "json"
)

type Header struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}
