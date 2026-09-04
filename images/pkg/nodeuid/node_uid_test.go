// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package nodeuid

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

	if got := resolveNodeUidViaKubernetesApi(context.Background(), "node-1"); got != "" {
		t.Errorf("resolveNodeUidViaKubernetesApi() = %q, want an empty string outside of a cluster", got)
	}
}

// stubResolver replaces the Kubernetes API interaction for the duration of the test and resets the package state, so
// that a resolved UID does not leak into the next test.
func stubResolver(t *testing.T, resolver func(context.Context, string) string) {
	t.Helper()
	t.Cleanup(SetResolverForTest(resolver))
}

func TestNodeUidWithoutPrefetch(t *testing.T) {
	stubResolver(t, func(context.Context, string) string {
		t.Error("the resolver must not be called when Prefetch was never called")
		return "uid-a"
	})

	if got := GetNodeUid(time.Second); got != "" {
		t.Errorf("NodeUid() = %q, want an empty string when no lookup was started", got)
	}
}

func TestPrefetchIgnoresAnEmptyNodeName(t *testing.T) {
	stubResolver(t, func(context.Context, string) string {
		t.Error("the resolver must not be called for an empty node name")
		return "uid-a"
	})

	Prefetch(context.Background(), "")

	if got := GetNodeUid(time.Second); got != "" {
		t.Errorf("NodeUid() = %q, want an empty string for an empty node name", got)
	}
}

func TestPrefetchResolvesTheNodeUid(t *testing.T) {
	stubResolver(t, func(_ context.Context, nodeName string) string {
		if nodeName != "node-a" {
			t.Errorf("resolver called with node name %q, want %q", nodeName, "node-a")
		}
		return "uid-a"
	})

	Prefetch(context.Background(), "node-a")

	if got := GetNodeUid(time.Second); got != "uid-a" {
		t.Errorf("NodeUid() = %q, want %q", got, "uid-a")
	}
}

func TestNodeUidGivesUpAfterTheWaitTimeout(t *testing.T) {
	release := make(chan struct{})
	// Deferred, not registered via t.Cleanup: the lookup has to be released before stubResolver's cleanup can wait
	// for it to finish.
	defer close(release)
	stubResolver(t, func(context.Context, string) string {
		<-release
		return "uid-a"
	})

	Prefetch(context.Background(), "node-a")

	start := time.Now()
	got := GetNodeUid(10 * time.Millisecond)
	elapsed := time.Since(start)

	if got != "" {
		t.Errorf("NodeUid() = %q, want an empty string while the lookup is still running", got)
	}
	if elapsed > time.Second {
		t.Errorf("NodeUid() blocked for %s, want it to give up after ~10ms", elapsed)
	}
}

func TestASuccessfulLookupHappensOnlyOnce(t *testing.T) {
	var calls atomic.Int32
	stubResolver(t, func(context.Context, string) string {
		calls.Add(1)
		return "uid-a"
	})

	for range 3 {
		Prefetch(context.Background(), "node-a")
		if got := GetNodeUid(time.Second); got != "uid-a" {
			t.Fatalf("NodeUid() = %q, want %q", got, "uid-a")
		}
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("the resolver ran %d times, want exactly 1 after a successful lookup", got)
	}
}

func TestAFailedLookupIsRetriedByTheNextPrefetch(t *testing.T) {
	var calls atomic.Int32
	stubResolver(t, func(context.Context, string) string {
		// Fail on the first attempt, succeed afterwards.
		if calls.Add(1) == 1 {
			return ""
		}
		return "uid-a"
	})

	Prefetch(context.Background(), "node-a")
	if got := GetNodeUid(time.Second); got != "" {
		t.Fatalf("NodeUid() = %q, want an empty string after a failed lookup", got)
	}

	Prefetch(context.Background(), "node-a")
	if got := GetNodeUid(time.Second); got != "uid-a" {
		t.Errorf("NodeUid() = %q, want %q after the retry succeeded", got, "uid-a")
	}
}

func TestConcurrentPrefetchStartsOneLookup(t *testing.T) {
	var calls atomic.Int32
	proceed := make(chan struct{})
	stubResolver(t, func(context.Context, string) string {
		calls.Add(1)
		<-proceed
		return "uid-a"
	})

	var waitGroup sync.WaitGroup
	for range 16 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			Prefetch(context.Background(), "node-a")
		}()
	}
	waitGroup.Wait()
	close(proceed)

	if got := GetNodeUid(time.Second); got != "uid-a" {
		t.Fatalf("NodeUid() = %q, want %q", got, "uid-a")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("the resolver ran %d times, want exactly 1 for concurrent Prefetch calls", got)
	}
}

// inClusterFixture stands up a TLS server that impersonates the Kubernetes API and points the package at a service
// account volume that trusts it, so that resolveNodeUidViaKubernetesApi can be exercised end to end: reading the
// token, building the CA-verifying HTTP client, and making the request. It returns the server so that the test can
// inspect what arrived.
func inClusterFixture(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	serverUrl, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("cannot parse the test server URL %q: %v", server.URL, err)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", serverUrl.Hostname())
	t.Setenv("KUBERNETES_SERVICE_PORT", serverUrl.Port())

	caCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	writeServiceAccountVolume(t, "service-account-token\n", caCert)

	return server
}

// writeServiceAccountVolume points serviceAccountTokenPath and serviceAccountCACertPath at temporary files holding the
// given content. An empty token or CA cert leaves that file absent, which is how the paths behave in a pod without a
// mounted service account.
func writeServiceAccountVolume(t *testing.T, token string, caCert []byte) {
	t.Helper()
	directory := t.TempDir()

	originalTokenPath := serviceAccountTokenPath
	originalCACertPath := serviceAccountCACertPath
	t.Cleanup(func() {
		serviceAccountTokenPath = originalTokenPath
		serviceAccountCACertPath = originalCACertPath
	})

	serviceAccountTokenPath = filepath.Join(directory, "token")
	if token != "" {
		if err := os.WriteFile(serviceAccountTokenPath, []byte(token), 0o600); err != nil {
			t.Fatalf("cannot write the token file: %v", err)
		}
	}

	serviceAccountCACertPath = filepath.Join(directory, "ca.crt")
	if len(caCert) > 0 {
		if err := os.WriteFile(serviceAccountCACertPath, caCert, 0o600); err != nil {
			t.Fatalf("cannot write the CA cert file: %v", err)
		}
	}
}

func TestResolveNodeUidViaKubernetesApi(t *testing.T) {
	var requestPath, authorizationHeader string
	inClusterFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.EscapedPath()
		authorizationHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"metadata":{"uid":"uid-a"}}`)
	})

	if got := resolveNodeUidViaKubernetesApi(context.Background(), "node-a"); got != "uid-a" {
		t.Errorf("resolveNodeUidViaKubernetesApi() = %q, want %q", got, "uid-a")
	}
	if requestPath != "/api/v1/nodes/node-a" {
		t.Errorf("request path = %q, want %q", requestPath, "/api/v1/nodes/node-a")
	}
	// The trailing newline of the token file must not end up in the header.
	if authorizationHeader != "Bearer service-account-token" {
		t.Errorf("Authorization header = %q, want %q", authorizationHeader, "Bearer service-account-token")
	}
}

func TestResolveNodeUidViaKubernetesApiFailures(t *testing.T) {
	unreadableHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"kind":"Status","code":403}`)
	}
	okHandler := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"metadata":{"uid":"uid-a"}}`)
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		// breakVolume replaces the service account volume the fixture set up.
		breakVolume func(t *testing.T, caCert []byte)
	}{
		{
			name:    "the API server denies the request",
			handler: unreadableHandler,
		},
		{
			name:    "the service account token is not mounted",
			handler: okHandler,
			breakVolume: func(t *testing.T, caCert []byte) {
				writeServiceAccountVolume(t, "", caCert)
			},
		},
		{
			name:    "the cluster CA certificate is not mounted",
			handler: okHandler,
			breakVolume: func(t *testing.T, _ []byte) {
				writeServiceAccountVolume(t, "service-account-token", nil)
			},
		},
		{
			name:    "the cluster CA certificate cannot be parsed",
			handler: okHandler,
			breakVolume: func(t *testing.T, _ []byte) {
				writeServiceAccountVolume(t, "service-account-token", []byte("this is not a PEM certificate"))
			},
		},
		{
			name:    "the API server presents a certificate the cluster CA did not sign",
			handler: okHandler,
			breakVolume: func(t *testing.T, _ []byte) {
				writeServiceAccountVolume(t, "service-account-token", unrelatedCACert(t))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := inClusterFixture(t, tt.handler)
			if tt.breakVolume != nil {
				caCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
				tt.breakVolume(t, caCert)
			}

			if got := resolveNodeUidViaKubernetesApi(context.Background(), "node-a"); got != "" {
				t.Errorf("resolveNodeUidViaKubernetesApi() = %q, want an empty string", got)
			}
		})
	}
}

// unrelatedCACert returns a PEM-encoded certificate of a CA that did not sign the test server's certificate, so that
// the TLS handshake fails verification rather than the connection failing outright.
func unrelatedCACert(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cannot generate a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cannot create a certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
