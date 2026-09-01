// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package nodeuid resolves the UID of the Kubernetes node a process runs on. It is a separate module, with no
// dependencies beyond the standard library, so that it can be linked into the OpenTelemetry collector image without
// interfering with any of the collector's Go dependency versions.
package nodeuid // import "github.com/dash0hq/dash0-operator/images/pkg/nodeuid"

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	serviceAccountTokenPath  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// nodeUidLookupTimeout bounds the request to the Kubernetes API, so that a slow or unreachable API server cannot
	// stall the startup of the process that initializes the OTel SDK.
	nodeUidLookupTimeout = 5 * time.Second

	// nodeUidResponseSizeLimit caps how much of the API server response is read. The metadata of a node is a few
	// kilobytes, the limit only guards against an unexpectedly large response body.
	nodeUidResponseSizeLimit = 1 << 20

	// partialObjectMetadataAcceptHeader asks the API server for the metadata of the node only, instead of the whole node
	// object, whose status.images list alone can be hundreds of kilobytes. API servers that do not support it fall back
	// to the plain application/json alternative, which contains metadata.uid as well.
	partialObjectMetadataAcceptHeader = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1, application/json"

	nodeUidUnavailableMsgTemplate = "%s The k8s.node.uid resource attribute will not be set.\n"
)

var (
	nodeUidMutex sync.Mutex

	// resolvedNodeUid stores the node uid if it has been resolved, so a successful lookup happens at most once process
	resolvedNodeUid string

	// lookupInFlight is non-nil while a background lookup runs and is closed when that lookup finishes.
	lookupInFlight chan struct{}

	// resolveNodeUid is a variable so that tests can stub out the interaction with the Kubernetes API.
	resolveNodeUid = resolveNodeUidViaKubernetesApi
)

// Prefetch starts resolving the UID of the node with the given name in the background, so that the result is already
// available when GetNodeUid is called. It returns immediately and never blocks the caller.
//
// Call it as early in the process lifetime as possible: the lookup then overlaps with the remaining startup work,
// which is what keeps GetNodeUid from waiting at all in the common case. It is safe to call repeatedly and from multiple
// goroutines. Calls are ignored while a lookup is in flight and once one has succeeded, so a process performs at most
// one successful lookup. A failed attempt is not remembered, hence a later call can still retry - in the background,
// so the retry never delays the caller either.
//
// Prefetch does nothing when the node name is empty, in which case GetNodeUid returns an empty string.
func Prefetch(ctx context.Context, nodeName string) {
	if nodeName == "" {
		return
	}

	nodeUidMutex.Lock()
	defer nodeUidMutex.Unlock()
	if resolvedNodeUid != "" || lookupInFlight != nil {
		return
	}

	done := make(chan struct{})
	lookupInFlight = done
	resolver := resolveNodeUid
	go func() {
		nodeUid := resolver(ctx, nodeName)

		nodeUidMutex.Lock()
		defer nodeUidMutex.Unlock()
		if nodeUid != "" {
			resolvedNodeUid = nodeUid
		}
		lookupInFlight = nil
		close(done)
	}()
}

// GetNodeUid returns the UID of the node the process runs on, or an empty string if it is not available. It waits at
// most waitTimeout for a lookup started by Prefetch to finish, and returns an empty string when that deadline passes,
// when no lookup was ever started, or when the lookup failed.
//
// The result is best-effort by design: the k8s.node.uid resource attribute is worth having, but not worth delaying the
// caller for, so telemetry without it is preferable to a stalled startup.
func GetNodeUid(waitTimeout time.Duration) string {
	nodeUidMutex.Lock()
	if resolvedNodeUid != "" {
		defer nodeUidMutex.Unlock()
		return resolvedNodeUid
	}
	done := lookupInFlight
	nodeUidMutex.Unlock()

	if done == nil {
		// Nothing has been resolved and nothing is running, there is nothing to wait for.
		return ""
	}

	timeout := time.NewTimer(waitTimeout)
	defer timeout.Stop()
	select {
	case <-done:
	case <-timeout.C:
		return ""
	}

	nodeUidMutex.Lock()
	defer nodeUidMutex.Unlock()
	return resolvedNodeUid
}

// SetResolverForTest replaces the interaction with the Kubernetes API and resets the resolution state, so that a test
// can drive Prefetch and GetNodeUid without a cluster. It returns a function that restores both, which the caller must
// invoke when the test ends. Restoring waits for a lookup that is still running, because that lookup writes the
// resolution state when it finishes and would otherwise leak a UID into the next test.
//
// This is exported only because the collector's internal-telemetry factory lives in a module of its own and therefore
// cannot reach the unexported resolver. Do not call it from production code.
func SetResolverForTest(resolver func(ctx context.Context, nodeName string) string) func() {
	nodeUidMutex.Lock()
	original := resolveNodeUid
	resolveNodeUid = resolver
	resolvedNodeUid = ""
	nodeUidMutex.Unlock()

	return func() {
		nodeUidMutex.Lock()
		done := lookupInFlight
		nodeUidMutex.Unlock()
		if done != nil {
			<-done
		}

		nodeUidMutex.Lock()
		defer nodeUidMutex.Unlock()
		resolveNodeUid = original
		resolvedNodeUid = ""
		lookupInFlight = nil
	}
}

// resolveNodeUidViaKubernetesApi returns the UID of the node with the given name, or an empty string if it cannot be
// determined.
//
// Unlike the node name, the node UID is not available via the Kubernetes downward API, so it has to be read from the
// Kubernetes API. The lookup is best-effort: self-monitoring telemetry is still useful without the k8s.node.uid
// attribute, hence every failure only produces a log message. Callers need the "get" permission on nodes.
func resolveNodeUidViaKubernetesApi(ctx context.Context, nodeName string) string {
	apiServerBaseUrl := inClusterApiServerBaseUrl()
	if apiServerBaseUrl == "" {
		// The process does not run inside a Kubernetes cluster, there is no API server to ask.
		return ""
	}
	token, err := os.ReadFile(serviceAccountTokenPath)
	if err != nil {
		log.Printf(
			nodeUidUnavailableMsgTemplate,
			fmt.Sprintf("Cannot read the service account token at %s: %v.", serviceAccountTokenPath, err),
		)
		return ""
	}
	httpClient, err := newInClusterHttpClient()
	if err != nil {
		log.Printf(
			nodeUidUnavailableMsgTemplate,
			fmt.Sprintf("Cannot create an HTTP client for the Kubernetes API: %v.", err),
		)
		return ""
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, nodeUidLookupTimeout)
	defer cancel()
	nodeUid, err := fetchNodeUid(
		timeoutCtx,
		httpClient,
		apiServerBaseUrl,
		strings.TrimSpace(string(token)),
		nodeName,
	)
	if err != nil {
		log.Printf(
			nodeUidUnavailableMsgTemplate,
			fmt.Sprintf("Cannot look up the UID of the node %s: %v.", nodeName, err),
		)
		return ""
	}

	return nodeUid
}

// inClusterApiServerBaseUrl returns the base URL of the Kubernetes API server, derived from the environment variables
// that the kubelet injects into every container. It returns an empty string when they are absent, which means the
// process does not run inside a Kubernetes cluster.
func inClusterApiServerBaseUrl() string {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", net.JoinHostPort(host, port))
}

// newInClusterHttpClient creates an HTTP client that verifies the certificate of the Kubernetes API server against the
// cluster CA from the service account volume.
func newInClusterHttpClient() (*http.Client, error) {
	caCert, err := os.ReadFile(serviceAccountCACertPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", serviceAccountCACertPath, err)
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("no certificates could be parsed from %s", serviceAccountCACertPath)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    rootCAs,
				MinVersion: tls.VersionTLS12,
			},
		},
	}, nil
}

// fetchNodeUid reads the UID of the node with the given name from the Kubernetes API.
func fetchNodeUid(
	ctx context.Context,
	httpClient *http.Client,
	apiServerBaseUrl string,
	token string,
	nodeName string,
) (string, error) {
	requestUrl := fmt.Sprintf("%s/api/v1/nodes/%s", apiServerBaseUrl, url.PathEscape(nodeName))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	request.Header.Set("Accept", partialObjectMetadataAcceptHeader)

	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s from %s", response.Status, requestUrl)
	}

	var node struct {
		Metadata struct {
			Uid string `json:"uid"`
		} `json:"metadata"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, nodeUidResponseSizeLimit)).Decode(&node); err != nil {
		return "", fmt.Errorf("cannot parse the response from %s: %w", requestUrl, err)
	}
	if node.Metadata.Uid == "" {
		return "", fmt.Errorf("the response from %s does not contain metadata.uid", requestUrl)
	}
	return node.Metadata.Uid, nil
}
