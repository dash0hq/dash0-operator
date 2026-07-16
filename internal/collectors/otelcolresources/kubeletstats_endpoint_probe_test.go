// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	probeTestNodeName = "test-node-name-123456789ab"
	probeTestNodeIp   = "10.1.2.34"
	probeTestNodeIpV6 = "fd00:1:2:3::9"
)

var (
	probeTestNodeNameEndpoint = fmt.Sprintf("https://%s:10250%s", probeTestNodeName, kubeletStatsSummaryPath)
	probeTestNodeIpEndpoint   = fmt.Sprintf("https://%s:10250%s", probeTestNodeIp, kubeletStatsSummaryPath)
	probeTestReadOnlyEndpoint = fmt.Sprintf("http://%s:10255%s", probeTestNodeIp, kubeletStatsSummaryPath)

	// The IPv6 node IP endpoints are bracketed via net.JoinHostPort, matching what probeNodeIpEndpoints builds.
	probeTestNodeIpV6Endpoint   = fmt.Sprintf("https://%s%s", net.JoinHostPort(probeTestNodeIpV6, "10250"), kubeletStatsSummaryPath)
	probeTestReadOnlyV6Endpoint = fmt.Sprintf("http://%s%s", net.JoinHostPort(probeTestNodeIpV6, "10255"), kubeletStatsSummaryPath)

	probeTestTlsError = &url.Error{
		Op:  "Get",
		URL: "https://host:10250/stats/summary",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}
	probeTestGenericError = fmt.Errorf(`Get "https://host:10250/stats/summary": dial tcp: connection refused`)
)

// fakeKubeletStatsEndpointProbes builds a kubeletStatsEndpointProbes backed by lookup tables. Any endpoint absent
// from the result maps is treated as a generic (non-TLS) probe error. A nil value in the result maps denotes a
// successful probe. Note that DNS resolution failures for the node name also surface as generic probe errors, since
// the probe request resolves the node name itself.
func fakeKubeletStatsEndpointProbes(
	verifiedResults map[string]error,
	insecureResults map[string]error,
) kubeletStatsEndpointProbes {
	return kubeletStatsEndpointProbes{
		sendProbeRequest: func(endpoint string) error {
			if err, ok := verifiedResults[endpoint]; ok {
				return err
			}
			return probeTestGenericError
		},
		sendProbeRequestInsecure: func(endpoint string) error {
			if err, ok := insecureResults[endpoint]; ok {
				return err
			}
			return probeTestGenericError
		},
	}
}

var _ = Describe("probeKubeletStatsEndpoint", func() {
	logger := logd.FromContext(context.Background())
	collectorConfig := util.CollectorConfig{NodeName: probeTestNodeName, NodeIp: probeTestNodeIp}

	It("disables the receiver when neither node name nor node IP is available", func() {
		result, ok := executeProbes(
			util.CollectorConfig{}, fakeKubeletStatsEndpointProbes(nil, nil), logger)
		Expect(ok).To(BeFalse())
		Expect(result.Enabled).To(BeFalse())
	})

	It("prefers the node IP endpoint over the node name endpoint when both certificates verify", func() {
		// Both endpoints verify. The node IP endpoint is equally secure but does not depend on DNS resolution at
		// scrape time at all, so it is preferred.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: nil,
				probeTestNodeIpEndpoint:   nil,
			},
			nil,
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeIpEndpoint))
		Expect(result.InsecureSkipVerify).To(BeFalse())
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
	})

	It("prefers the insecure node IP endpoint over the node name endpoint with verified TLS", func() {
		// The node IP endpoint's certificate does not verify, the node name endpoint's does. The node IP endpoint
		// with insecure_skip_verify is still preferred: it does not depend on DNS resolution at scrape time, and the
		// scrape traffic never leaves the node, so the missing certificate verification is not a security issue in
		// practice.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: nil,
				probeTestNodeIpEndpoint:   probeTestTlsError,
			},
			map[string]error{probeTestNodeIpEndpoint: nil},
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeIpEndpoint))
		Expect(result.InsecureSkipVerify).To(BeTrue())
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
	})

	It("prefers the insecure node IP endpoint over the unencrypted read-only endpoint", func() {
		// The secure node IP probe fails with a TLS error, but both the insecure node IP probe and the read-only
		// endpoint would succeed. The insecure node IP endpoint must be preferred: it stays encrypted and
		// authenticated (serviceAccount token, RBAC), whereas the read-only port is plaintext HTTP with no
		// authentication.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeIpEndpoint:   probeTestTlsError,
				probeTestReadOnlyEndpoint: nil,
			},
			map[string]error{probeTestNodeIpEndpoint: nil},
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeIpEndpoint))
		Expect(result.InsecureSkipVerify).To(BeTrue())
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
	})

	It("prefers the read-only endpoint over the node name endpoint with verified TLS", func() {
		// The port 10250 endpoints are not reachable via the node IP, but the read-only endpoint is, and the node
		// name endpoint's certificate would even verify. The read-only endpoint is still preferred, because it does
		// not depend on DNS resolution at scrape time. It transmits no credentials whatsoever (auth_type: none), and
		// the unencrypted scrape traffic never leaves the node, so the plaintext HTTP transport is not a security
		// issue in practice.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: nil,
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestReadOnlyEndpoint: nil,
			},
			nil,
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsReadOnlyEndpoint))
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeNone))
		Expect(result.InsecureSkipVerify).To(BeFalse())
	})

	It("uses the read-only endpoint when no other node IP based endpoint is viable", func() {
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestReadOnlyEndpoint: nil,
			},
			nil,
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsReadOnlyEndpoint))
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeNone))
	})

	It("falls back to the node name endpoint when no node IP based endpoint is viable", func() {
		// No node IP based endpoint works at all, but the node name endpoint's certificate verifies. The node name
		// endpoint is then used, accepting the dependency on DNS resolution at scrape time.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{probeTestNodeNameEndpoint: nil},
			nil,
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeNameEndpoint))
		Expect(result.InsecureSkipVerify).To(BeFalse())
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
	})

	It("falls back to the insecure node name endpoint as a last resort", func() {
		// No node IP based endpoint works at all, and the node name endpoint's certificate does not verify, but the
		// node name endpoint does accept the request with insecure_skip_verify. The node name endpoint with
		// insecure_skip_verify is then used as the last resort rather than disabling the receiver.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: probeTestTlsError,
				probeTestNodeIpEndpoint:   probeTestTlsError,
				probeTestReadOnlyEndpoint: probeTestGenericError,
			},
			map[string]error{
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestNodeNameEndpoint: nil,
			},
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeNameEndpoint))
		Expect(result.InsecureSkipVerify).To(BeTrue())
	})

	It("disables the receiver when the node name cert does not verify and insecure_skip_verify also fails", func() {
		// No node IP based endpoint works at all, and the node name endpoint's certificate does not verify either.
		// Unlike a mere certificate verification failure, the insecure_skip_verify probe request itself also fails
		// (e.g. the endpoint is not reachable at all under a different network path, or rejects the request for a
		// reason unrelated to TLS verification), so the node name endpoint must not be used and the receiver is
		// disabled instead of blindly trusting insecure_skip_verify.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: probeTestTlsError,
				probeTestNodeIpEndpoint:   probeTestTlsError,
				probeTestReadOnlyEndpoint: probeTestGenericError,
			},
			map[string]error{
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestNodeNameEndpoint: probeTestGenericError,
			},
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeFalse())
		Expect(result.Enabled).To(BeFalse())
	})

	It("disables the receiver when the node name probe fails for a non-TLS reason", func() {
		// No node IP based endpoint works, and the node name probe fails with a generic (non-TLS) error, which means
		// the endpoint is not reachable (this includes DNS resolution failures for the node name). The receiver is
		// disabled.
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeNameEndpoint: probeTestGenericError,
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestReadOnlyEndpoint: probeTestGenericError,
			},
			nil,
		)
		result, ok := executeProbes(collectorConfig, probes, logger)
		Expect(ok).To(BeFalse())
		Expect(result.Enabled).To(BeFalse())
	})

	It("skips the node IP based endpoints and uses the node name endpoint when only the node name is available", func() {
		// The node IP is not available at all, only the node name is. executeProbes must skip the node IP based
		// endpoints entirely (the collectorConfig.NodeIp != "" guard) and go straight to the node name endpoint.
		nodeNameOnlyConfig := util.CollectorConfig{NodeName: probeTestNodeName}
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{probeTestNodeNameEndpoint: nil},
			nil,
		)
		result, ok := executeProbes(nodeNameOnlyConfig, probes, logger)
		Expect(ok).To(BeTrue())
		Expect(result.Enabled).To(BeTrue())
		Expect(result.Endpoint).To(Equal(kubeletStatsNodeNameEndpoint))
		Expect(result.InsecureSkipVerify).To(BeFalse())
		Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
	})

	It("disables the receiver when only the node IP is available and all node IP based probes fail", func() {
		// The node name is not available at all, only the node IP is. executeProbes must skip the node name endpoint
		// (the collectorConfig.NodeName != "" guard); once all node IP based probes fail, the receiver is disabled
		// rather than falling back to a node name endpoint that cannot be built.
		nodeIpOnlyConfig := util.CollectorConfig{NodeIp: probeTestNodeIp}
		probes := fakeKubeletStatsEndpointProbes(
			map[string]error{
				probeTestNodeIpEndpoint:   probeTestGenericError,
				probeTestReadOnlyEndpoint: probeTestGenericError,
			},
			nil,
		)
		result, ok := executeProbes(nodeIpOnlyConfig, probes, logger)
		Expect(ok).To(BeFalse())
		Expect(result.Enabled).To(BeFalse())
	})

	Context("with an IPv6 node IP", func() {
		// The node IP is a raw IPv6 address. probeNodeIpEndpoints must enclose it in brackets, both in the probe
		// request URLs and in the resulting endpoint config, so the node IP based endpoints stay usable on IPv6
		// clusters instead of falling through to the DNS-dependent node name endpoint.
		ipv6CollectorConfig := util.CollectorConfig{NodeName: probeTestNodeName, NodeIp: probeTestNodeIpV6}

		It("uses the bracketed node IP endpoint with verified TLS", func() {
			probes := fakeKubeletStatsEndpointProbes(
				map[string]error{probeTestNodeIpV6Endpoint: nil},
				nil,
			)
			result, ok := executeProbes(ipv6CollectorConfig, probes, logger)
			Expect(ok).To(BeTrue())
			Expect(result.Enabled).To(BeTrue())
			Expect(result.Endpoint).To(Equal(kubeletStatsNodeIpEndpointIpv6))
			Expect(result.InsecureSkipVerify).To(BeFalse())
			Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
		})

		It("falls back to the bracketed node IP endpoint with insecure_skip_verify", func() {
			probes := fakeKubeletStatsEndpointProbes(
				map[string]error{probeTestNodeIpV6Endpoint: probeTestTlsError},
				map[string]error{probeTestNodeIpV6Endpoint: nil},
			)
			result, ok := executeProbes(ipv6CollectorConfig, probes, logger)
			Expect(ok).To(BeTrue())
			Expect(result.Enabled).To(BeTrue())
			Expect(result.Endpoint).To(Equal(kubeletStatsNodeIpEndpointIpv6))
			Expect(result.InsecureSkipVerify).To(BeTrue())
			Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeServiceAccount))
		})

		It("falls back to the bracketed read-only node IP endpoint", func() {
			probes := fakeKubeletStatsEndpointProbes(
				map[string]error{
					probeTestNodeIpV6Endpoint:   probeTestGenericError,
					probeTestReadOnlyV6Endpoint: nil,
				},
				nil,
			)
			result, ok := executeProbes(ipv6CollectorConfig, probes, logger)
			Expect(ok).To(BeTrue())
			Expect(result.Enabled).To(BeTrue())
			Expect(result.Endpoint).To(Equal(kubeletStatsReadOnlyEndpointIpv6))
			Expect(result.InsecureSkipVerify).To(BeFalse())
			Expect(result.AuthType).To(Equal(kubeletStatsAuthTypeNone))
		})
	})
})
