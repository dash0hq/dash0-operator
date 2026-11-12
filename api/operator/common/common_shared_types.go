// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

// This file contains common types used in multiple custom resource definitions (e.g. Dash0Monitoring and
// Dash0OperatorConfiguration), and potentially also in multiple versions of those.

type ConditionType string

const (
	ConditionTypeAvailable ConditionType = "Available"
	ConditionTypeDegraded  ConditionType = "Degraded"
	RedactedValue                        = "<redacted>"
)

// Export describes the observability backend to which telemetry data will be sent: Dash0, an OTLP-compatible receiver
// via gRPC or HTTP, or a combination of these three exporters. At least one exporter has to be defined.
//
// +kubebuilder:validation:MinProperties=1
type Export struct {
	// Settings for sending telemetry to Dash0.
	//
	// +kubebuilder:validation:Optional
	Dash0 *Dash0Configuration `json:"dash0,omitempty"`

	// Settings for sending telemetry to an OTLP-compatible receiver via HTTP.
	//
	// +kubebuilder:validation:Optional
	Http *HttpConfiguration `json:"http,omitempty"`

	// Settings for sending telemetry to an OTLP-compatible receiver via gRPC.
	//
	// +kubebuilder:validation:Optional
	Grpc *GrpcConfiguration `json:"grpc,omitempty"`
}

// Dash0Configuration describes to which Dash0 ingress endpoint telemetry data will be sent.
type Dash0Configuration struct {
	// The OTLP/gRPC endpoint of your Dash0 organization, see
	// https://app.dash0.com/goto/settings/endpoints?endpoint_type=otlp_grpc. It starts with `ingress.` and ends in
	// `dash0.com:4317`.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// The identifier (not the display name) of the Dash0 dataset to which telemetry data will be sent, see
	// https://app.dash0.com/settings/datasets. If omitted, Dash0 selects the dataset based on the authorization token,
	// and API access (dashboards, check rules, etc.) uses the dataset "default".
	Dataset string `json:"dataset,omitempty"`

	// The authorization settings for sending data to Dash0. Exactly one of token and secretRef has to be provided.
	//
	// +kubebuilder:validation:Required
	Authorization Authorization `json:"authorization"`

	// The API endpoint of your Dash0 organization, see
	// https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http. It starts with `https://api.` and ends in
	// `.dash0.com`. It is not used for telemetry, but for managing dashboards, check rules, synthetic checks and views
	// via the operator.
	//
	// +kubebuilder:validation:Optional
	ApiEndpoint string `json:"apiEndpoint,omitempty"`

	// The client-side gRPC keepalive settings. There is usually no need to configure them.
	//
	// +kubebuilder:validation:Optional
	Keepalive *KeepaliveClientConfig `json:"keepalive,omitempty"`
}

// Authorization contains the authorization settings for Dash0.
//
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:MaxProperties=1
type Authorization struct {
	// The Dash0 authorization token, see https://app.dash0.com/settings/auth-tokens.
	//
	// +kubebuilder:validation:Optional
	Token *string `json:"token,omitempty"` // either token or secret ref, with token taking precedence

	// A reference to a Kubernetes secret containing the Dash0 authorization token.
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
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each HTTP request, for example for authorization.
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
	// The URL of the OTLP-compatible receiver to which telemetry data will be sent.
	//
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Additional headers to be sent with each gRPC request, for example for authorization.
	//
	// +kubebuilder:validation:Optional
	Headers []Header `json:"headers,omitempty"`

	// Whether to disable TLS. By default, TLS is used unless the endpoint starts with 'http://'.
	//
	// +kubebuilder:validation:Optional
	Insecure *bool `json:"insecure,omitempty"`

	// Whether to skip verifying the server's certificate chain. It is a validation error to combine this with
	// `insecure=true`, which disables TLS altogether.
	//
	// +kubebuilder:validation:Optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`

	// The client-side gRPC keepalive settings. There is usually no need to configure them.
	//
	// +kubebuilder:validation:Optional
	Keepalive *KeepaliveClientConfig `json:"keepalive,omitempty"`

	// The load balancer to use, round_robin or pick_first. When omitted, the collector's default (round_robin) is used.
	//
	// +kubebuilder:validation:Optional
	BalancerName BalancerName `json:"balancer_name,omitempty"`
}

// OtlpEncoding describes the encoding of the OTLP data when sent via HTTP.
//
// +kubebuilder:validation:Enum=proto;json
type OtlpEncoding string

const (
	Proto OtlpEncoding = "proto"
	Json  OtlpEncoding = "json"
)

// Header describes a single header to be sent with each request. Exactly one of value and valueFrom has to be provided.
//
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value or valueFrom has to be provided for a header"
type Header struct {
	// The name of the header.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// The value of the header as a literal string.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value,omitempty"`

	// The value of the header, sourced from a Kubernetes secret. Use this for sensitive values like authorization tokens
	// or API keys.
	//
	// +kubebuilder:validation:Optional
	ValueFrom *HeaderValueFrom `json:"valueFrom,omitempty"`
}

// HeaderValueFrom describes a source for the value of a header.
type HeaderValueFrom struct {
	// A reference to a key in a Kubernetes secret containing the header value.
	//
	// +kubebuilder:validation:Required
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef"`
}

// SecretKeySelector selects a key of a Kubernetes secret.
type SecretKeySelector struct {
	// The name of the secret containing the value.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// The key within the secret which contains the value.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// KeepaliveClientConfig exposes the keepalive.ClientParameters for gRPC clients.
type KeepaliveClientConfig struct {
	// Period after which a keepalive ping is sent on the transport.
	// +kubebuilder:validation:Optional
	Time *string `json:"time,omitempty"`

	// Time to wait for a keepalive ping acknowledgement.
	// +kubebuilder:validation:Optional
	Timeout *string `json:"timeout,omitempty"`

	// Allow keepalive pings when there are no active streams.
	// +kubebuilder:validation:Optional
	PermitWithoutStream *bool `json:"permit_without_stream,omitempty"`
}

// BalancerName describes the load balancer used by the OTLP gRPC exporter.
// See https://github.com/grpc/grpc-go/blob/master/examples/features/load_balancing/README.md
//
// +kubebuilder:validation:Enum=round_robin;pick_first
type BalancerName string

const (
	RoundRobin BalancerName = "round_robin"
	PickFirst  BalancerName = "pick_first"
)

func (e *Export) HasDash0ExportConfigured() bool {
	return e != nil && e.Dash0 != nil
}

func (e *Export) HasDash0ApiAccessConfigured() bool {
	return e.HasDash0ExportConfigured() &&
		e.Dash0.ApiEndpoint != "" &&
		(e.Dash0.Authorization.Token != nil || e.Dash0.Authorization.SecretRef != nil)
}

// CountExports counts the total number of different exports (dash0, grpc, http) in the given slice of Export elements.
func CountExports(exports []Export) int {
	count := 0
	for _, export := range exports {
		if export.Dash0 != nil {
			count++
		}
		if export.Grpc != nil {
			count++
		}
		if export.Http != nil {
			count++
		}
	}
	return count
}

// ToExports is a small helper function to convert a single Export into Exports with a single element.
func (e *Export) ToExports() []Export {
	if e == nil {
		return nil
	}
	return []Export{
		*e,
	}
}

// Redact removes a literal authorization token, if any is set. The authorization is modified in place, make sure to
// only call it on a resource that is not intended to be used for actual exporting/authentication later.
func (a *Authorization) Redact() {
	if a.Token != nil && len(*a.Token) > 0 {
		a.Token = new(RedactedValue)
	}
}

// Redact removes the authorization token and header values from the export. The export is modified in place, make sure
// to only call it on exports that are not intended to be used for actual exporting later.
func (e *Export) Redact() {
	if e.Dash0 != nil {
		e.Dash0.Authorization.Redact()
	}
	if e.Http != nil {
		// Any header value can potentially be a secret, so we redact all values. Headers whose values are sourced from a
		// secret (valueFrom) carry are left untouched.
		for i := range e.Http.Headers {
			if e.Http.Headers[i].Value != "" {
				e.Http.Headers[i].Value = RedactedValue
			}
		}
	}
	if e.Grpc != nil {
		for i := range e.Grpc.Headers {
			if e.Grpc.Headers[i].Value != "" {
				e.Grpc.Headers[i].Value = RedactedValue
			}
		}
	}
}
