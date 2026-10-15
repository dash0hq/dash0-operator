// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	webhookCertificatesSecretName = "dash0-operator-webhook-server-cert"
	caBundleInjectionLabel        = "dash0.com/inject-ca-bundle"
)

type caBundleTarget struct {
	kind     string
	name     string
	jsonPath string
}

var (
	caBundleTargets = []caBundleTarget{
		{kind: "mutatingwebhookconfiguration", name: "dash0-operator-injector"},
		{kind: "mutatingwebhookconfiguration", name: "dash0-operator-operator-configuration-mutating"},
		{kind: "mutatingwebhookconfiguration", name: "dash0-operator-monitoring-mutating"},
		{kind: "validatingwebhookconfiguration", name: "dash0-operator-operator-configuration-validator"},
		{kind: "validatingwebhookconfiguration", name: "dash0-operator-monitoring-validator"},
		{
			kind:     "customresourcedefinition",
			name:     "dash0monitorings.operator.dash0.com",
			jsonPath: "{.spec.conversion.webhook.clientConfig.caBundle}",
		},
	}

	// dash0WebhookCallFailure matches the errors the API server returns when it cannot call one of the operator's
	// admission webhooks or its conversion webhook.
	dash0WebhookCallFailure = regexp.MustCompile(
		`failed calling webhook "[^"]*\.dash0\.com"|conversion webhook for operator\.dash0\.com/`,
	)

	// certificateFailure matches the errors of webhook calls that fail because the webhook's serving certificate is not
	// trusted by the caBundle.
	certificateFailure = regexp.MustCompile(`x509: |tls: failed to verify certificate`)
)

func (t caBundleTarget) caBundleJsonPath() string {
	if t.jsonPath != "" {
		return t.jsonPath
	}
	return "{.webhooks[0].clientConfig.caBundle}"
}

// readCaBundles returns the base64-encoded caBundle of every target, keyed by kind/name.
func readCaBundles(g Gomega) map[string]string {
	caBundles := map[string]string{}
	for _, target := range caBundleTargets {
		output, err := run(exec.Command(
			"kubectl",
			"get",
			target.kind,
			target.name,
			"-o",
			"jsonpath="+target.caBundleJsonPath(),
		), false)
		g.Expect(err).ToNot(HaveOccurred())
		caBundles[target.kind+"/"+target.name] = strings.TrimSpace(output)
	}
	return caBundles
}

// readWebhookCertificatesSecretField returns a base64-encoded field of the Secret that holds the operator-managed
// webhook certificates.
func readWebhookCertificatesSecretField(g Gomega, operatorNamespace string, key string) string {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"secret",
		"--namespace",
		operatorNamespace,
		webhookCertificatesSecretName,
		"-o",
		fmt.Sprintf("jsonpath={.data.%s}", strings.ReplaceAll(key, ".", "\\.")),
	), false)
	g.Expect(err).ToNot(HaveOccurred())
	return strings.TrimSpace(output)
}

// verifyCaBundlesCarryOperatorManagedBundle checks that every target carries the CA bundle from the operator-managed
// Secret, and returns that bundle (base64-encoded).
func verifyCaBundlesCarryOperatorManagedBundle(operatorNamespace string) string {
	var bundle string
	Eventually(func(g Gomega) {
		bundle = readWebhookCertificatesSecretField(g, operatorNamespace, "ca.crt")
		g.Expect(bundle).ToNot(BeEmpty())
		for target, caBundle := range readCaBundles(g) {
			g.Expect(caBundle).To(Equal(bundle), "unexpected caBundle in %s", target)
		}
	}, 60*time.Second, time.Second).Should(Succeed())
	return bundle
}

func removeCaBundleFromMonitoringValidator() {
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"patch",
		"validatingwebhookconfiguration",
		"dash0-operator-monitoring-validator",
		"--type",
		"json",
		"-p",
		`[{"op":"remove","path":"/webhooks/0/clientConfig/caBundle"}]`,
	))).To(Succeed())
}

func removeWebhookCertificatesSecretData(operatorNamespace string) {
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"patch",
		"secret",
		"--namespace",
		operatorNamespace,
		webhookCertificatesSecretName,
		"--type",
		"json",
		"-p",
		`[{"op":"remove","path":"/data"}]`,
	))).To(Succeed())
}

func verifyOperatorManagedCertificatesAreNotUsed(operatorNamespace string) {
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"get",
		"secret",
		"--namespace",
		operatorNamespace,
		webhookCertificatesSecretName,
	), false, false, false)).ToNot(Succeed())
	for _, target := range caBundleTargets {
		output, err := run(exec.Command(
			"kubectl",
			"get",
			target.kind,
			target.name,
			"-o",
			fmt.Sprintf("jsonpath={.metadata.labels.%s}", strings.ReplaceAll(caBundleInjectionLabel, ".", "\\.")),
		), false)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.TrimSpace(output)).To(BeEmpty(), "unexpected injection label on %s/%s", target.kind, target.name)
	}
}

func rollbackOperator(operatorNamespace string) {
	By("rolling back the operator Helm release to the previous revision")
	Expect(runAndIgnoreOutput(exec.Command(
		"helm",
		"rollback",
		"--wait",
		"--namespace",
		operatorNamespace,
		operatorHelmReleaseName,
	))).To(Succeed())
}

// readDash0MonitoringResourceAsV1Alpha1 reads the monitoring resource in version v1alpha1, which requires a call to the
// conversion webhook, since monitoring resources are stored as v1beta1.
func readDash0MonitoringResourceAsV1Alpha1(g Gomega, namespace string) {
	g.Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"get",
		"--namespace",
		namespace,
		"dash0monitorings.v1alpha1.operator.dash0.com",
		dash0MonitoringResourceName,
	))).To(Succeed())
}

// webhookProbe repeatedly sends dry-run requests that need the operator's admission webhooks and its conversion
// webhook, and records every request that the API server could not deliver to them.
type webhookProbe struct {
	stop     chan struct{}
	done     chan struct{}
	attempts int
	failures []string
}

// startWebhookProbe starts a probe that records every failed webhook call. With onlyCertificateFailures, it only
// records failures caused by the certificates, for example while operator pods terminate, which stop serving their
// webhooks during shutdown.
func startWebhookProbe(namespace string, onlyCertificateFailures bool) *webhookProbe {
	By("starting to probe the operator's webhooks continuously")
	manifests := []string{
		fmt.Sprintf(`apiVersion: operator.dash0.com/v1beta1
kind: Dash0Monitoring
metadata:
  name: %s
  namespace: %s
`, util.WebhookReachabilityCheckResourceName, namespace),
		// creating a v1alpha1 resource requires a conversion to the storage version v1beta1
		fmt.Sprintf(`apiVersion: operator.dash0.com/v1alpha1
kind: Dash0Monitoring
metadata:
  name: %s
  namespace: %s
`, util.WebhookReachabilityCheckResourceName, namespace),
		fmt.Sprintf(`apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: %s
`, util.WebhookReachabilityCheckResourceName),
	}
	probe := &webhookProbe{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer GinkgoRecover()
		defer close(probe.done)
		for {
			for _, manifest := range manifests {
				command := exec.Command("kubectl", "create", "--dry-run=server", "-f", "-")
				command.Stdin = strings.NewReader(manifest)
				// Other rejections (for example by the validation webhooks of an older operator version) are expected.
				output, err := run(command, false, false, false)
				probe.attempts++
				if err != nil &&
					dash0WebhookCallFailure.MatchString(output) &&
					(!onlyCertificateFailures || certificateFailure.MatchString(output)) {
					probe.failures = append(probe.failures, output)
				}
			}
			select {
			case <-probe.stop:
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return probe
}

func (p *webhookProbe) stopAndVerifyNoFailures() {
	By("verifying that the operator's webhooks have been reachable all the time")
	close(p.stop)
	<-p.done
	Expect(p.attempts).To(BeNumerically(">", 0))
	Expect(p.failures).To(BeEmpty())
}
