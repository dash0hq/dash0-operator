// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type endpointProbeFn func(util.CollectorConfig, logd.Logger) (util.KubeletStatsReceiverConfig, bool)

const (
	kubeletStatsNodeIpEndpoint       = "https://${env:K8S_NODE_IP}:10250"
	kubeletStatsNodeIpEndpointIpv6   = "https://[${env:K8S_NODE_IP}]:10250"
	kubeletStatsReadOnlyEndpoint     = "http://${env:K8S_NODE_IP}:10255"
	kubeletStatsReadOnlyEndpointIpv6 = "http://[${env:K8S_NODE_IP}]:10255"
	kubeletStatsNodeNameEndpoint     = "https://${env:K8S_NODE_NAME}:10250"

	kubeletStatsAuthTypeServiceAccount   = "serviceAccount"
	kubeletStatsAuthTypeNone             = "none"
	kubeletStatsSummaryPath              = "/stats/summary"
	usingKubeletStatsReceiverEndpointMsg = "Using %s as kubeletstats receiver endpoint."

	// serviceAccountCACertPath is the CA bundle the kubeletstats receiver adds to the system cert pool when verifying
	// TLS certificates with auth_type: serviceAccount (see the saClientProvider in
	// https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/internal/kubelet/client.go). The
	// TLS-verifying probe requests must use the same trust anchors, otherwise the probe outcome does not predict the
	// receiver's behavior: kubelet serving certificates are signed by the cluster CA (if at all), never by a public CA,
	// so verifying against the system cert pool alone would fail even on clusters where the receiver could verify the
	// certificate.
	serviceAccountCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// probeRequestTimeout bounds each probe request, so that an unreachable (e.g. blackholed) endpoint does not stall
	// the collector reconciliation for the duration of the OS-level TCP timeout.
	probeRequestTimeout = 5 * time.Second
)

var (
	secureClientOnce sync.Once
	secureClient     *http.Client

	insecureClient = &http.Client{
		Timeout: probeRequestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	configNodeIpSecureTls = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeIpEndpoint,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: false,
	}
	configNodeIpSecureTlsIpv6 = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeIpEndpointIpv6,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: false,
	}

	configNodeIpInsecureSkipVerify = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeIpEndpoint,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: true,
	}
	configNodeIpInsecureSkipVerifyIpv6 = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeIpEndpointIpv6,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: true,
	}

	configNodeIpReadOnlyEndpointUnencrypted = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsReadOnlyEndpoint,
		AuthType:           kubeletStatsAuthTypeNone,
		InsecureSkipVerify: false,
	}
	configNodeIpReadOnlyEndpointUnencryptedIpv6 = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsReadOnlyEndpointIpv6,
		AuthType:           kubeletStatsAuthTypeNone,
		InsecureSkipVerify: false,
	}

	configNodeNameSecureTls = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeNameEndpoint,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: false,
	}

	configNodeNameInsecureSkipVerify = util.KubeletStatsReceiverConfig{
		Enabled:            true,
		Endpoint:           kubeletStatsNodeNameEndpoint,
		AuthType:           kubeletStatsAuthTypeServiceAccount,
		InsecureSkipVerify: true,
	}

	configDisabled = util.KubeletStatsReceiverConfig{Enabled: false}
)

// kubeletStatsEndpointProbes bundles the external interactions performed while probing for a viable kubeletstats
// receiver endpoint. This exists so the steps can be replaced in unit tests.
type kubeletStatsEndpointProbes struct {
	// sendProbeRequest performs an HTTP GET; for https endpoints, TLS certificates are verified against the system cert
	// pool plus the cluster CA at serviceAccountCACertPath, matching the trust anchors of the kubeletstats receiver
	// with auth_type: serviceAccount. Note: the probe sends no bearer token while the kubeletstat receiver does. This
	// asymmetry does not affect outcomes, because executeHttpRequest only fails on transport errors, a 401 response is
	// counted as a successful probe.
	sendProbeRequest func(endpoint string) error
	// sendProbeRequestInsecure performs an HTTP GET with TLS certificate verification disabled. It is used to check if
	// falling back to insecure_skip_verify is viable.
	sendProbeRequestInsecure func(endpoint string) error
}

// probeKubeletStatsEndpoint determines the best endpoint for the kubeletstats receiver configuration, depending on the
// specifics of the cluster (TLS certificates, available ports, ...).
func probeKubeletStatsEndpoint(
	collectorConfig util.CollectorConfig,
	logger logd.Logger,
) (util.KubeletStatsReceiverConfig, bool) {
	httpClient := getSecureProbeClient(logger)
	probes := kubeletStatsEndpointProbes{
		sendProbeRequest:         func(endpoint string) error { return executeHttpRequest(httpClient, endpoint) },
		sendProbeRequestInsecure: func(endpoint string) error { return executeHttpRequest(insecureClient, endpoint) },
	}
	return executeProbes(collectorConfig, probes, logger)
}

// getSecureProbeClient lazily creates (and then reuses) the HTTP client for the TLS-verifying probe requests, trusting
// the system cert pool plus the cluster CA at serviceAccountCACertPath (see the comment there). If the cluster CA
// cannot be loaded, the client degrades to verifying against the system cert pool only, which in practice means the
// TLS probes will fail and the probe falls back to the insecure_skip_verify / read-only options.
func getSecureProbeClient(logger logd.Logger) *http.Client {
	secureClientOnce.Do(func() {
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			logger.Info(fmt.Sprintf("Cannot load the system cert pool: %v.", err))
			rootCAs = x509.NewCertPool()
		}
		if caCert, err := os.ReadFile(serviceAccountCACertPath); err != nil {
			logger.Info(fmt.Sprintf(
				"Cannot read %s (%v), TLS probe requests will verify certificates against the system cert pool only.",
				serviceAccountCACertPath,
				err,
			))
		} else if !rootCAs.AppendCertsFromPEM(caCert) {
			logger.Info(fmt.Sprintf(
				"No certificates could be parsed from %s, TLS probe requests will verify certificates against the "+
					"system cert pool only.",
				serviceAccountCACertPath,
			))
		}
		secureClient = &http.Client{
			Timeout: probeRequestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: rootCAs},
			},
		}
	})
	return secureClient
}

// executeProbes runs different probes to determine the best endpoint for the kubeletstats receiver configuration,
// depending on the specifics of the cluster (TLS certificates, available ports, ...).
//
// Options in order of preference:
// 1. Node IP with verified TLS
// 2. Node IP with insecure_skip_verify
// 3. Node IP with read-only port (:10255) (no auth, no encryption)
// 4. Node name with verified TLS
// 5. Node name with insecure_skip_verify
// 6. Receiver disabled
//
// Every node IP based option is preferred over any node name based option because the node IP options do not depend
// on DNS at scrape time: the collector pods run with the default dnsPolicy (ClusterFirst), so resolving the node name
// requires the cluster DNS service (e.g. CoreDNS) to be healthy, and -- since node names are never part of the
// cluster-local DNS zone -- also the upstream resolver behind it, at every connection (re-)establishment. Cluster DNS
// outages are a common incident class, and kubelet metrics are most valuable exactly during incidents, so not
// depending on DNS at all ranks higher than a stricter TLS posture.
//
// Preferring insecure_skip_verify (option 2) or even the unencrypted, unauthenticated read-only endpoint (option 3)
// over the node name based options is not a security issue in practice: each collector daemonset pod scrapes the
// kubelet of the node it runs on (K8S_NODE_IP is the pod's status.hostIP, provided via the downward API). Traffic
// from a pod to its own node's IP travels from the pod's network namespace via a veth pair directly into the host's
// network stack and never leaves the node. (We run the collectors with hostNetwork: false.)
// Intercepting or redirecting it therefore requires root-level access to that very node -- and an attacker with that
// level of access already controls the kubelet and its TLS certificates, so TLS certificate verification would not
// protect against them. In detail:
//   - With insecure_skip_verify (options 2 and 5), the connection is still encrypted -- the TLS key exchange defeats
//     passive eavesdropping even without certificate verification -- and the client still authenticates itself to the
//     kubelet via its service account token. The residual risk is sending that bearer token to a server whose
//     identity has not been verified, which, per the above, is only exploitable by an attacker who already controls
//     the node.
//   - The read-only endpoint (option 3) uses plaintext HTTP, but transmits no credential at all (auth_type: none), so
//     there is nothing to steal; in that respect it is even less risky than insecure_skip_verify. The payload that
//     could theoretically be observed (again: only from within the node itself) is low-sensitivity metadata -- pod,
//     container and volume names plus resource usage. Whether the read-only port is enabled at all is a property of
//     the cluster, not of this configuration: if it is enabled, every pod in the cluster can read from it anyway, so
//     scraping it does not add any exposure. Note that the read-only port is mostly a legacy option that is usually not
//     enabled on modern clusters.
func executeProbes(
	collectorConfig util.CollectorConfig,
	probes kubeletStatsEndpointProbes,
	logger logd.Logger,
) (util.KubeletStatsReceiverConfig, bool) {
	if collectorConfig.NodeName == "" && collectorConfig.NodeIp == "" {
		logger.Warn(
			"No K8s_NODE_NAME and no K8S_NODE_IP available, skipping kubeletstats receiver endpoint lookup. " +
				"The kubeletstats receiver will be disabled. Some Kubernetes infrastructure metrics will be missing.",
		)
		return configDisabled, false
	}

	if collectorConfig.NodeIp != "" {
		if config, ok := probeNodeIpEndpoints(collectorConfig, probes, logger); ok {
			return config, true
		}
	} else {
		logger.Info("Skipping all node IP based kubeletstats receiver endpoints, the node IP is not available.")
	}

	if collectorConfig.NodeName != "" {
		if config, ok := probeNodeNameEndpoint(collectorConfig, probes, logger); ok {
			return config, true
		}
	} else {
		logger.Info("Skipping the node name based kubeletstats receiver endpoints, the node name is not available.")
	}

	logger.Warn(
		"No viable kubeletstats receiver endpoint found. The kubeletstats receiver will be disabled. Some Kubernetes " +
			"infrastructure metrics will be missing.",
	)
	return configDisabled, false
}

// probeNodeIpEndpoints probes the node IP based endpoints in their order of preference: verified TLS on port 10250,
// insecure_skip_verify on port 10250, and finally the unencrypted read-only port 10255.
//
// The caller guarantees collectorConfig.NodeIp is non-empty.
func probeNodeIpEndpoints(
	collectorConfig util.CollectorConfig,
	probes kubeletStatsEndpointProbes,
	logger logd.Logger,
) (util.KubeletStatsReceiverConfig, bool) {
	logger.Info(fmt.Sprintf("Checking the address family of the node IP %s.", collectorConfig.NodeIp))
	isIPv6 := util.IsIPv6Address(collectorConfig.NodeIp)
	if isIPv6 {
		logger.Info(fmt.Sprintf("The node IP %s is an IPv6 address.", collectorConfig.NodeIp))
	} else {
		logger.Info(fmt.Sprintf("The node IP %s is an IPv4 address.", collectorConfig.NodeIp))
	}
	configSecure := configNodeIpSecureTls
	configInsecure := configNodeIpInsecureSkipVerify
	configReadOnly := configNodeIpReadOnlyEndpointUnencrypted
	if isIPv6 {
		configSecure = configNodeIpSecureTlsIpv6
		configInsecure = configNodeIpInsecureSkipVerifyIpv6
		configReadOnly = configNodeIpReadOnlyEndpointUnencryptedIpv6
	}

	nodeIpEndpointWithPath :=
		fmt.Sprintf("https://%s%s", net.JoinHostPort(collectorConfig.NodeIp, "10250"), kubeletStatsSummaryPath)
	logger.Info(fmt.Sprintf("Attempting probe request to %s.", nodeIpEndpointWithPath))
	nodeIpProbeErr := probes.sendProbeRequest(nodeIpEndpointWithPath)
	if nodeIpProbeErr == nil {
		logger.Info(fmt.Sprintf("Probe request to %s has been successful.", nodeIpEndpointWithPath))
		logger.Info(fmt.Sprintf(usingKubeletStatsReceiverEndpointMsg, configSecure.Endpoint))
		return configSecure, true
	}

	if isTlsError(nodeIpProbeErr) {
		logger.Info(
			fmt.Sprintf(
				"Failed to verify certificate for %s. (%s)",
				nodeIpEndpointWithPath,
				nodeIpProbeErr.Error(),
			))
		// The node IP endpoint did not pass the TLS probe. Try it again with insecure_skip_verify before falling back
		// to the read-only port: the connection stays encrypted and authenticated (serviceAccount token, RBAC), which
		// is a smaller compromise than the plaintext, unauthenticated read-only port.
		logger.Info(
			fmt.Sprintf("Attempting probe request to %s without TLS certificate verification.", nodeIpEndpointWithPath),
		)
		nodeIpInsecureProbeErr := probes.sendProbeRequestInsecure(nodeIpEndpointWithPath)
		if nodeIpInsecureProbeErr == nil {
			logger.Info(
				fmt.Sprintf(
					"Probe request to %s without TLS certificate verification has been successful.",
					nodeIpEndpointWithPath,
				))
			logger.Info(fmt.Sprintf(
				"Using %s as kubeletstats receiver endpoint with insecure_skip_verify: true.",
				configInsecure.Endpoint,
			))
			return configInsecure, true
		}
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s without TLS certificate verification resulted in an error: %s",
				nodeIpEndpointWithPath,
				nodeIpInsecureProbeErr.Error(),
			))
	} else {
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s resulted in an error: %s.",
				nodeIpEndpointWithPath,
				nodeIpProbeErr.Error(),
			))
	}

	// Note: The read-only endpoint is plain HTTP, so no TLS is involved in this probe request, despite using the
	// TLS-verifying client.
	readOnlyNodeIpEndpointWithPath :=
		fmt.Sprintf("http://%s%s", net.JoinHostPort(collectorConfig.NodeIp, "10255"), kubeletStatsSummaryPath)
	logger.Info(fmt.Sprintf("Attempting probe request to read-only endpoint %s.", readOnlyNodeIpEndpointWithPath))
	nodeIpReadOnlyProbeErr := probes.sendProbeRequest(readOnlyNodeIpEndpointWithPath)
	if nodeIpReadOnlyProbeErr == nil {
		logger.Info(
			fmt.Sprintf(
				"Probe request to read-only endpoint %s has been successful.",
				readOnlyNodeIpEndpointWithPath,
			),
		)
		logger.Info(fmt.Sprintf(
			"Using read-only endpoint %s as kubeletstats receiver endpoint.",
			configReadOnly.Endpoint,
		))
		return configReadOnly, true
	}
	logger.Info(
		fmt.Sprintf(
			"The probe request to %s resulted in an error: %s. No viable node IP based endpoint found.",
			readOnlyNodeIpEndpointWithPath,
			nodeIpReadOnlyProbeErr.Error(),
		))
	return configDisabled, false
}

// probeNodeNameEndpoint probes the node name endpoint (https://${env:K8S_NODE_NAME}:10250), the last resort before
// disabling the receiver.
//
// No explicit DNS lookup for the node name is required: the probe request resolves the node name itself, and a
// resolution failure surfaces as a non-TLS error, which disqualifies the node name endpoint.
//
// The caller guarantees collectorConfig.NodeName is non-empty.
func probeNodeNameEndpoint(
	collectorConfig util.CollectorConfig,
	probes kubeletStatsEndpointProbes,
	logger logd.Logger,
) (util.KubeletStatsReceiverConfig, bool) {
	nodeNameEndpointWithPath := fmt.Sprintf("https://%s:10250%s", collectorConfig.NodeName, kubeletStatsSummaryPath)
	logger.Info(fmt.Sprintf("Attempting probe request to %s.", nodeNameEndpointWithPath))
	nodeNameProbeErr := probes.sendProbeRequest(nodeNameEndpointWithPath)
	if nodeNameProbeErr == nil {
		logger.Info(fmt.Sprintf("Probe request to %s has been successful.", nodeNameEndpointWithPath))
		logger.Info(fmt.Sprintf(usingKubeletStatsReceiverEndpointMsg, configNodeNameSecureTls.Endpoint))
		return configNodeNameSecureTls, true
	}

	if isTlsError(nodeNameProbeErr) {
		logger.Info(
			fmt.Sprintf(
				"Failed to verify certificate for %s. (%s)",
				nodeNameEndpointWithPath,
				nodeNameProbeErr.Error(),
			))
		logger.Info(
			fmt.Sprintf("Attempting probe request to %s without TLS certificate verification.", nodeNameEndpointWithPath),
		)
		nodeNameInsecureProbeErr := probes.sendProbeRequestInsecure(nodeNameEndpointWithPath)
		if nodeNameInsecureProbeErr == nil {
			logger.Info(
				fmt.Sprintf(
					"Probe request to %s without TLS certificate verification has been successful.",
					nodeNameEndpointWithPath,
				))
			logger.Info(fmt.Sprintf(
				"Using %s as kubeletstats receiver endpoint with insecure_skip_verify: true.",
				configNodeNameInsecureSkipVerify.Endpoint,
			))
			return configNodeNameInsecureSkipVerify, true
		}
		logger.Info(
			fmt.Sprintf(
				"The probe request to %s without TLS certificate verification resulted in an error: %s. The node name "+
					"endpoint cannot be used.",
				nodeNameEndpointWithPath,
				nodeNameInsecureProbeErr.Error(),
			))
		return configDisabled, false
	}
	// nodeNameTlsProbeErr != nil, but not a TLS error. This also includes the inability to resolve the node name via DNS.
	logger.Info(
		fmt.Sprintf(
			"The probe request to %s resulted in an error: %s. The node name endpoint cannot be used.",
			nodeNameEndpointWithPath,
			nodeNameProbeErr.Error(),
		))
	return configDisabled, false
}

func executeHttpRequest(httpClient *http.Client, endpoint string) error {
	res, err := httpClient.Get(endpoint)
	defer func() {
		if res != nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
	}()
	return err
}

// isTlsError reports whether err (or any error it wraps) is a TLS certificate verification failure.
func isTlsError(err error) bool {
	return errors.As(err, new(*tls.CertificateVerificationError))
}
