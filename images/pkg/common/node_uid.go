// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common // import "github.com/dash0hq/dash0-operator/images/pkg/common"

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
	nodeUidMutex    sync.Mutex
	resolvedNodeUid string
)

// resolveNodeUid returns the UID of the node with the given name, or an empty string if it cannot be determined.
//
// Unlike the node name, the node UID is not available via the Kubernetes downward API, so it has to be read from the
// Kubernetes API. The lookup is best-effort: self-monitoring telemetry is still useful without the k8s.node.uid
// attribute, hence every failure only produces a log message.
//
// A resolved UID is cached for the lifetime of the process, since the UID of a node never changes and a replaced node
// means a replaced pod. Failed lookups are not cached, so a later call can still succeed.
func resolveNodeUid(ctx context.Context, nodeName string) string {
	nodeUidMutex.Lock()
	defer nodeUidMutex.Unlock()

	if resolvedNodeUid != "" {
		return resolvedNodeUid
	}

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

	resolvedNodeUid = nodeUid
	return resolvedNodeUid
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
