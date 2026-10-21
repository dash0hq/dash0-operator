// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	testNow      = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	testDnsNames = servingDnsNames("dash0-operator-webhook-service", "dash0-system")
)

var _ = Describe("The operator-managed webhook certificate material", func() {

	Describe("generating new material", func() {
		It("creates a CA and a serving certificate that chains up to it", func() {
			m := generateTestMaterial(retentionCandidates{})

			Expect(m.activeCa.cert.IsCA).To(BeTrue())
			Expect(isOperatorCreated(m.activeCa.cert)).To(BeTrue())
			Expect(m.activeCa.cert.NotAfter).To(Equal(testNow.Add(caValidity).Truncate(time.Second)))
			Expect(m.serving.cert.DNSNames).To(Equal(testDnsNames))
			Expect(m.serving.cert.NotAfter).To(Equal(testNow.Add(servingCertValidity).Truncate(time.Second)))
			Expect(verifyServingCertChain(m.serving.cert, m.activeCa.cert, testNow)).To(Succeed())
			Expect(parseCertificates(m.bundle())).To(HaveLen(1))
		})

		It("retains the CA of older chart versions and other foreign certificates", func() {
			chartCa := createForeignCa(legacyChartCaCommonName, testNow.Add(365*24*time.Hour))
			certManagerCa := createForeignCa("cert-manager-ca", testNow.Add(90*24*time.Hour))
			candidates := retentionCandidates{}
			candidates.add([]*x509.Certificate{chartCa, certManagerCa}, false, testNow)

			m := generateTestMaterial(candidates)

			bundle := parseCertificates(m.bundle())
			Expect(bundle).To(HaveLen(3))
			Expect(containsCertificate(bundle, chartCa)).To(BeTrue())
			Expect(containsCertificate(bundle, certManagerCa)).To(BeTrue())
			Expect(m.retainedChartCas).To(HaveLen(1))
			Expect(m.retainedCerts).To(HaveLen(1))
			Expect(m.retainedSince).To(Equal(testNow))
		})
	})

	Describe("collecting retention candidates", func() {
		It("sorts certificates into chart CAs and other foreign certificates", func() {
			chartCaByName := createForeignCa(legacyChartCaCommonName, testNow.Add(time.Hour))
			chartCaFromSecret := createForeignCa("some-ca", testNow.Add(time.Hour))
			// cert-manager's selfSigned issuer injects a self-signed leaf certificate, not a CA
			selfSignedLeaf := createForeignCert("self-signed-leaf", testNow.Add(time.Hour), false)
			candidates := retentionCandidates{}

			candidates.add([]*x509.Certificate{chartCaByName, selfSignedLeaf, chartCaFromSecret}, false, testNow)
			candidates.add([]*x509.Certificate{chartCaFromSecret}, true, testNow)

			Expect(candidates.chartCas).To(HaveLen(2))
			Expect(containsCertificate(candidates.chartCas, chartCaByName)).To(BeTrue())
			Expect(containsCertificate(candidates.chartCas, chartCaFromSecret)).To(BeTrue())
			Expect(candidates.other).To(HaveLen(1))
			Expect(containsCertificate(candidates.other, selfSignedLeaf)).To(BeTrue())
		})

		It("skips expired certificates and certificates created by the operator", func() {
			expired := createForeignCa("expired", testNow.Add(-time.Hour))
			operatorCa, err := generateCa(testNow)
			Expect(err).ToNot(HaveOccurred())
			candidates := retentionCandidates{}

			candidates.add([]*x509.Certificate{expired, operatorCa.cert}, false, testNow)
			candidates.add([]*x509.Certificate{operatorCa.cert}, true, testNow)

			Expect(candidates.chartCas).To(BeEmpty())
			Expect(candidates.other).To(BeEmpty())
		})
	})

	Describe("parsing the secret data", func() {
		It("round-trips the material", func() {
			m := generateTestMaterial(retentionCandidates{
				chartCas: []*x509.Certificate{createForeignCa(legacyChartCaCommonName, testNow.Add(time.Hour))},
				other:    []*x509.Certificate{createForeignCa("other", testNow.Add(time.Hour))},
			})
			_, err := m.maintain(maintainInput{now: testNow, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			data := m.toSecretData()

			parsed, err := materialFromSecretData(data, testNow)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.toSecretData()).To(Equal(data))
		})

		It("rejects empty data", func() {
			_, err := materialFromSecretData(map[string][]byte{}, testNow)
			Expect(err).To(MatchError(ContainSubstring("no data")))
		})

		It("rejects an expired active CA", func() {
			m := generateTestMaterial(retentionCandidates{})
			_, err := materialFromSecretData(m.toSecretData(), testNow.Add(caValidity+time.Hour))
			Expect(err).To(MatchError(ContainSubstring("has expired")))
		})

		It("rejects an active CA whose key does not match", func() {
			m := generateTestMaterial(retentionCandidates{})
			other := generateTestMaterial(retentionCandidates{})
			data := m.toSecretData()
			data[keyActiveCaKey] = other.activeCa.keyPem
			_, err := materialFromSecretData(data, testNow)
			Expect(err).To(MatchError(ContainSubstring("does not match")))
		})

		It("drops a serving certificate that has not been signed by the active CA", func() {
			m := generateTestMaterial(retentionCandidates{})
			other := generateTestMaterial(retentionCandidates{})
			data := m.toSecretData()
			data[keyServingCert] = other.serving.certPem
			data[keyServingKey] = other.serving.keyPem

			parsed, err := materialFromSecretData(data, testNow)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.serving).To(BeNil())
		})

		It("accepts material whose validity starts in the future (clock skew between replicas)", func() {
			m := generateTestMaterial(retentionCandidates{})
			parsed, err := materialFromSecretData(m.toSecretData(), testNow.Add(-2*time.Hour))
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.serving).ToNot(BeNil())
		})
	})

	Describe("maintaining the material", func() {
		It("does not change anything for fresh material", func() {
			m := generateTestMaterial(retentionCandidates{})
			before := m.toSecretData()
			result, err := m.maintain(maintainInput{now: testNow, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.changes).To(BeEmpty())
			Expect(result.bundleChanged).To(BeFalse())
			Expect(m.toSecretData()).To(Equal(before))
		})

		It("renews the serving certificate shortly before it expires", func() {
			m := generateTestMaterial(retentionCandidates{})
			later := testNow.Add(servingCertValidity - servingCertRenewBefore + time.Hour)
			result, err := m.maintain(maintainInput{now: later, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.changes).To(ContainElement("renewed the serving certificate"))
			Expect(result.bundleChanged).To(BeFalse())
			Expect(m.serving.cert.NotAfter).To(Equal(later.Add(servingCertValidity).Truncate(time.Second)))
		})

		It("does not renew the serving certificate if the renewal would not extend its validity", func() {
			m := generateTestMaterial(retentionCandidates{})
			m.serving = mustIssueServingCert(m.activeCa, testDnsNames, testNow.Add(caValidity-servingCertValidity))
			// close to the expiry of the active CA, the validity of a new serving certificate would be capped
			later := testNow.Add(caValidity - 10*24*time.Hour)
			m.pendingCa = mustGenerateCa(later)
			m.rotationStartedAt = later
			servingCert := m.serving.cert

			result, err := m.maintain(maintainInput{now: later, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.changes).To(BeEmpty())
			Expect(m.serving.cert).To(Equal(servingCert))
		})

		It("adds missing DNS names to the existing ones", func() {
			m := generateTestMaterial(retentionCandidates{})
			otherNames := servingDnsNames("custom-webhook-service", "dash0-system")
			result, err := m.maintain(maintainInput{now: testNow, dnsNames: otherNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.changes).To(HaveLen(1))
			Expect(m.serving.cert.DNSNames).To(Equal(append(append([]string{}, testDnsNames...), otherNames...)))

			// a replica with the previous names does not change the certificate again
			servingCert := m.serving.cert
			result, err = m.maintain(maintainInput{now: testNow, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.changes).To(BeEmpty())
			Expect(m.serving.cert).To(Equal(servingCert))
		})

		It("starts a CA rotation one year before the active CA expires", func() {
			m := generateTestMaterial(retentionCandidates{})
			later := testNow.Add(caValidity - caRotationLeadTime + time.Hour)
			activeCa := m.activeCa.cert

			result, err := m.maintain(maintainInput{now: later, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeTrue())
			Expect(m.pendingCa).ToNot(BeNil())
			Expect(m.rotationStartedAt).To(Equal(later))
			Expect(m.activeCa.cert).To(Equal(activeCa))
			bundle := parseCertificates(m.bundle())
			Expect(bundle).To(HaveLen(2))
			Expect(containsCertificate(bundle, m.pendingCa.cert)).To(BeTrue())
			// the serving certificate is still issued by the active CA
			Expect(verifyServingCertChain(m.serving.cert, activeCa, later)).To(Succeed())
		})

		It("keeps retained foreign certificates for seven days and until the rollout is complete", func() {
			m := generateTestMaterial(retentionCandidates{
				other: []*x509.Certificate{createForeignCa("cert-manager-ca", testNow.Add(365*24*time.Hour))},
			})

			result, err := m.maintain(maintainInput{
				now: testNow.Add(foreignCertRetention - time.Hour), dnsNames: testDnsNames, rolloutComplete: alwaysComplete,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeFalse())
			Expect(m.retainedCerts).To(HaveLen(1))

			result, err = m.maintain(maintainInput{
				now:             testNow.Add(foreignCertRetention + time.Hour),
				dnsNames:        testDnsNames,
				rolloutComplete: func() bool { return false },
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeFalse())
			Expect(result.retentionOverdue).To(BeTrue())
			Expect(m.retainedCerts).To(HaveLen(1))

			result, err = m.maintain(maintainInput{
				now: testNow.Add(foreignCertRetention + time.Hour), dnsNames: testDnsNames, rolloutComplete: alwaysComplete,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeTrue())
			Expect(m.retainedCerts).To(BeEmpty())
			Expect(parseCertificates(m.bundle())).To(HaveLen(1))
		})

		It("keeps the CA of older chart versions when other foreign certificates are removed", func() {
			chartCa := createForeignCa(legacyChartCaCommonName, testNow.Add(30*24*time.Hour))
			m := generateTestMaterial(retentionCandidates{
				chartCas: []*x509.Certificate{chartCa},
				other:    []*x509.Certificate{createForeignCa("cert-manager-ca", testNow.Add(365*24*time.Hour))},
			})

			result, err := m.maintain(maintainInput{
				now: testNow.Add(20 * 24 * time.Hour), dnsNames: testDnsNames, rolloutComplete: alwaysComplete,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeTrue())
			Expect(m.retainedCerts).To(BeEmpty())
			Expect(m.retainedChartCas).To(ConsistOf(chartCa))
		})

		It("removes expired certificates only together with another bundle change", func() {
			chartCa := createForeignCa(legacyChartCaCommonName, testNow.Add(30*24*time.Hour))
			m := generateTestMaterial(retentionCandidates{chartCas: []*x509.Certificate{chartCa}})

			afterChartCaExpiry := testNow.Add(60 * 24 * time.Hour)
			result, err := m.maintain(maintainInput{now: afterChartCaExpiry, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeFalse())
			Expect(m.retainedChartCas).To(HaveLen(1))

			rotationStart := testNow.Add(caValidity - caRotationLeadTime + time.Hour)
			result, err = m.maintain(maintainInput{now: rotationStart, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.bundleChanged).To(BeTrue())
			Expect(m.retainedChartCas).To(BeEmpty())
		})
	})

	Describe("completing a CA rotation", func() {
		var m *material
		var rotationStart time.Time

		BeforeEach(func() {
			m = generateTestMaterial(retentionCandidates{})
			rotationStart = testNow.Add(caValidity - caRotationLeadTime + time.Hour)
			_, err := m.maintain(maintainInput{now: rotationStart, dnsNames: testDnsNames, rolloutComplete: alwaysComplete})
			Expect(err).ToNot(HaveOccurred())
		})

		It("reports that no rotation is pending", func() {
			fresh := generateTestMaterial(retentionCandidates{})
			Expect(fresh.evaluateSwitch(true, testNow)).To(Equal(noRotationPending))
		})

		It("waits for the minimum rotation age", func() {
			Expect(m.evaluateSwitch(true, rotationStart.Add(minimumRotationAge-time.Minute))).To(Equal(rotationWaiting))
		})

		It("is blocked while a target does not carry the pending CA", func() {
			Expect(m.evaluateSwitch(false, rotationStart.Add(minimumRotationAge+time.Minute))).To(Equal(rotationBlocked))
		})

		It("switches once all targets carry the pending CA", func() {
			Expect(m.evaluateSwitch(true, rotationStart.Add(minimumRotationAge+time.Minute))).To(Equal(rotationSwitch))
		})

		It("forces the switch shortly before the active CA expires", func() {
			forced := m.activeCa.cert.NotAfter.Add(-forcedSwitchLeadTime + time.Hour)
			Expect(m.evaluateSwitch(false, forced)).To(Equal(rotationForcedSwitch))
			Expect(m.evaluateSwitch(true, forced)).To(Equal(rotationSwitch))
		})

		It("makes the pending CA active without changing the bundle", func() {
			bundle := m.bundle()
			previousActive := m.activeCa.cert
			pending := m.pendingCa.cert
			switchTime := rotationStart.Add(minimumRotationAge + time.Minute)

			Expect(m.switchToPendingCa(testDnsNames, switchTime)).To(Succeed())

			Expect(m.activeCa.cert).To(Equal(pending))
			Expect(m.pendingCa).To(BeNil())
			Expect(m.previousCas).To(ConsistOf(previousActive))
			Expect(m.bundle()).To(Equal(bundle))
			Expect(verifyServingCertChain(m.serving.cert, pending, switchTime)).To(Succeed())

			parsed, err := materialFromSecretData(m.toSecretData(), switchTime)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.bundle()).To(Equal(bundle))
		})
	})

	Describe("parsing the injection targets", func() {
		It("parses a list of targets", func() {
			targets, err := ParseTargets(
				"MutatingWebhookConfiguration/a, ValidatingWebhookConfiguration/b,CustomResourceDefinition/c.example.com,",
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(targets).To(Equal([]Target{
				{Kind: TargetKindMutatingWebhookConfiguration, Name: "a"},
				{Kind: TargetKindValidatingWebhookConfiguration, Name: "b"},
				{Kind: TargetKindCustomResourceDefinition, Name: "c.example.com"},
			}))
		})

		It("returns no targets for an empty string", func() {
			targets, err := ParseTargets("")
			Expect(err).ToNot(HaveOccurred())
			Expect(targets).To(BeEmpty())
		})

		It("rejects an entry without a name", func() {
			_, err := ParseTargets("MutatingWebhookConfiguration")
			Expect(err).To(HaveOccurred())
		})

		It("rejects an unsupported kind", func() {
			_, err := ParseTargets("Secret/a")
			Expect(err).To(HaveOccurred())
		})
	})
})

func alwaysComplete() bool {
	return true
}

func generateTestMaterial(candidates retentionCandidates) *material {
	m, err := generateMaterial(testDnsNames, candidates, testNow)
	Expect(err).ToNot(HaveOccurred())
	return m
}

func mustGenerateCa(now time.Time) *keyPair {
	ca, err := generateCa(now)
	Expect(err).ToNot(HaveOccurred())
	return ca
}

func mustIssueServingCert(ca *keyPair, dnsNames []string, now time.Time) *keyPair {
	serving, err := issueServingCert(ca, dnsNames, now)
	Expect(err).ToNot(HaveOccurred())
	return serving
}

// createForeignCa creates a self-signed CA without the operator marker, like the CA of older chart versions or a
// cert-manager CA.
func createForeignCa(commonName string, notAfter time.Time) *x509.Certificate {
	return createForeignCert(commonName, notAfter, true)
}

func createForeignCert(commonName string, notAfter time.Time, isCa bool) *x509.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).ToNot(HaveOccurred())
	serialNumber, err := randomSerialNumber()
	Expect(err).ToNot(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             testNow.Add(-365 * 24 * time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  isCa,
	}
	pair, err := createKeyPair(template, template, key, key)
	Expect(err).ToNot(HaveOccurred())
	return pair.cert
}
