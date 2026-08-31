// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchNodeUid(t *testing.T) {
	tests := []struct {
		name         string
		nodeName     string
		statusCode   int
		responseBody string
		want         string
		wantErr      bool
	}{
		{
			name:         "reads metadata.uid",
			nodeName:     "node-1",
			statusCode:   http.StatusOK,
			responseBody: `{"metadata":{"name":"node-1","uid":"2b0f1cbe-4d68-4a1e-9c3f-6f9d3b1a7e55"}}`,
			want:         "2b0f1cbe-4d68-4a1e-9c3f-6f9d3b1a7e55",
		},
		{
			name:         "ignores the remainder of a full node object",
			nodeName:     "node-1",
			statusCode:   http.StatusOK,
			responseBody: `{"kind":"Node","metadata":{"uid":"node-uid"},"status":{"images":[{"names":["image-1"]}]}}`,
			want:         "node-uid",
		},
		{
			name:         "fails when the node cannot be read",
			nodeName:     "node-1",
			statusCode:   http.StatusForbidden,
			responseBody: `{"kind":"Status","code":403}`,
			wantErr:      true,
		},
		{
			name:         "fails on a malformed response",
			nodeName:     "node-1",
			statusCode:   http.StatusOK,
			responseBody: `this is not JSON`,
			wantErr:      true,
		},
		{
			name:         "fails when metadata.uid is absent",
			nodeName:     "node-1",
			statusCode:   http.StatusOK,
			responseBody: `{"metadata":{"name":"node-1"}}`,
			wantErr:      true,
		},
		{
			name:         "escapes the node name",
			nodeName:     "node/../secret",
			statusCode:   http.StatusOK,
			responseBody: `{"metadata":{"uid":"node-uid"}}`,
			want:         "node-uid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestPath, authorizationHeader, acceptHeader string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestPath = r.URL.EscapedPath()
				authorizationHeader = r.Header.Get("Authorization")
				acceptHeader = r.Header.Get("Accept")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = fmt.Fprint(w, tt.responseBody)
			}))
			defer server.Close()

			got, err := fetchNodeUid(
				context.Background(),
				server.Client(),
				server.URL,
				"service-account-token",
				tt.nodeName,
			)

			if tt.wantErr {
				if err == nil {
					t.Errorf("fetchNodeUid() error = nil, want an error")
				}
			} else if err != nil {
				t.Errorf("fetchNodeUid() error = %v, want no error", err)
			}
			if got != tt.want {
				t.Errorf("fetchNodeUid() = %q, want %q", got, tt.want)
			}

			wantPath := fmt.Sprintf("/api/v1/nodes/%s", tt.nodeName)
			if tt.nodeName == "node/../secret" {
				// The node name must not be able to escape the /api/v1/nodes/ path segment.
				wantPath = "/api/v1/nodes/node%2F..%2Fsecret"
			}
			if requestPath != wantPath {
				t.Errorf("request path = %q, want %q", requestPath, wantPath)
			}
			if authorizationHeader != "Bearer service-account-token" {
				t.Errorf("Authorization header = %q, want %q", authorizationHeader, "Bearer service-account-token")
			}
			if acceptHeader != partialObjectMetadataAcceptHeader {
				t.Errorf("Accept header = %q, want %q", acceptHeader, partialObjectMetadataAcceptHeader)
			}
		})
	}
}

func TestFetchNodeUidFailsOnAnUnreachableApiServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	serverUrl := server.URL
	server.Close()

	if _, err := fetchNodeUid(context.Background(), client, serverUrl, "token", "node-1"); err == nil {
		t.Errorf("fetchNodeUid() error = nil, want an error")
	}
}

func TestInClusterApiServerBaseUrl(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{
			name: "no host and no port",
			want: "",
		},
		{
			name: "host without port",
			host: "10.96.0.1",
			want: "",
		},
		{
			name: "port without host",
			port: "443",
			want: "",
		},
		{
			name: "IPv4 host",
			host: "10.96.0.1",
			port: "443",
			want: "https://10.96.0.1:443",
		},
		{
			name: "IPv6 host",
			host: "fd00::1",
			port: "443",
			want: "https://[fd00::1]:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBERNETES_SERVICE_HOST", tt.host)
			t.Setenv("KUBERNETES_SERVICE_PORT", tt.port)

			if got := inClusterApiServerBaseUrl(); got != tt.want {
				t.Errorf("inClusterApiServerBaseUrl() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveNodeUidOutsideOfACluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	if got := resolveNodeUid(context.Background(), "node-1"); got != "" {
		t.Errorf("resolveNodeUid() = %q, want an empty string outside of a cluster", got)
	}
}

func TestAssembleResourceOmitsTheNodeUidWhenItCannotBeResolved(t *testing.T) {
	t.Setenv("K8S_NODE_NAME", "node-1")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	resourceAttributes := assembleResource(context.Background(), "service-name", "", "", "", "", "", "")

	if got := attributeValue(resourceAttributes.Attributes(), "k8s.node.name"); got != "node-1" {
		t.Errorf("resource attribute \"k8s.node.name\" = %q, want %q", got, "node-1")
	}
	if got := attributeValue(resourceAttributes.Attributes(), "k8s.node.uid"); got != "" {
		t.Errorf("resource attribute \"k8s.node.uid\" = %q, want it to be absent", got)
	}
}
