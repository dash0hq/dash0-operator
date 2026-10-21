// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"slices"
	"time"
)

const (
	caValidity             = 10 * 365 * 24 * time.Hour
	servingCertValidity    = 365 * 24 * time.Hour
	servingCertRenewBefore = 90 * 24 * time.Hour
	caRotationLeadTime     = 365 * 24 * time.Hour
	forcedSwitchLeadTime   = 30 * 24 * time.Hour
	minimumRotationAge     = 24 * time.Hour
	foreignCertRetention   = 7 * 24 * time.Hour
	expiryWarningThreshold = 30 * 24 * time.Hour
	backdate               = time.Hour

	// operatorMarkerOrganization marks CAs created by the operator. CAs without it (the CA of older chart versions, a
	// cert-manager CA) are foreign and are only retained temporarily.
	operatorMarkerOrganization = "dash0-operator-managed"
	caCommonName               = "dash0-operator-webhook-ca"
	// legacyChartCaCommonName is the common name of the CA that older chart versions generated on every render.
	legacyChartCaCommonName = "dash0-operator-ca"

	pemTypeCertificate = "CERTIFICATE"
	pemTypePrivateKey  = "PRIVATE KEY"
)

// keyPair is a certificate together with its private key, in parsed and in PEM form.
type keyPair struct {
	cert    *x509.Certificate
	certPem []byte
	key     crypto.Signer
	keyPem  []byte
}

func generateCa(now time.Time) (*keyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("cannot generate the CA key: %w", err)
	}
	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   caCommonName,
			Organization: []string{operatorMarkerOrganization},
		},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	return createKeyPair(template, template, key, key)
}

// issueServingCert issues a serving certificate for the given DNS names, signed by the given CA. Its validity is capped
// at the NotAfter of the CA.
func issueServingCert(ca *keyPair, dnsNames []string, now time.Time) (*keyPair, error) {
	if len(dnsNames) == 0 {
		return nil, fmt.Errorf("cannot issue a serving certificate without DNS names")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("cannot generate the serving certificate key: %w", err)
	}
	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   dnsNames[0],
			Organization: []string{operatorMarkerOrganization},
		},
		DNSNames:              dnsNames,
		NotBefore:             now.Add(-backdate),
		NotAfter:              servingCertNotAfter(ca, now),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	return createKeyPair(template, ca.cert, key, ca.key)
}

func servingCertNotAfter(ca *keyPair, now time.Time) time.Time {
	notAfter := now.Add(servingCertValidity)
	if ca.cert.NotAfter.Before(notAfter) {
		return ca.cert.NotAfter
	}
	return notAfter
}

func createKeyPair(
	template *x509.Certificate,
	parent *x509.Certificate,
	key *ecdsa.PrivateKey,
	signer crypto.Signer,
) (*keyPair, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, key.Public(), signer)
	if err != nil {
		return nil, fmt.Errorf("cannot create the certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("cannot parse the created certificate: %w", err)
	}
	keyDer, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("cannot encode the private key: %w", err)
	}
	return &keyPair{
		cert:    cert,
		certPem: pem.EncodeToMemory(&pem.Block{Type: pemTypeCertificate, Bytes: der}),
		key:     key,
		keyPem:  pem.EncodeToMemory(&pem.Block{Type: pemTypePrivateKey, Bytes: keyDer}),
	}, nil
}

func randomSerialNumber() (*big.Int, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("cannot generate a serial number: %w", err)
	}
	return serialNumber, nil
}

// parseKeyPair parses a single certificate and its private key and checks that they belong together.
func parseKeyPair(certPem []byte, keyPem []byte) (*keyPair, error) {
	certs := parseCertificates(certPem)
	if len(certs) != 1 {
		return nil, fmt.Errorf("expected exactly one certificate, found %d", len(certs))
	}
	key, err := parsePrivateKey(keyPem)
	if err != nil {
		return nil, err
	}
	publicKey, ok := key.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !publicKey.Equal(certs[0].PublicKey) {
		return nil, fmt.Errorf("the private key does not match the certificate")
	}
	return &keyPair{
		cert:    certs[0],
		certPem: encodeCertificates(certs),
		key:     key,
		keyPem:  keyPem,
	}, nil
}

func parsePrivateKey(keyPem []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(keyPem)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in the private key data")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, fmt.Errorf("unsupported private key type %T", key)
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("cannot parse the private key")
}

// parseCertificates returns all certificates from the given PEM data, skipping blocks that cannot be parsed.
func parseCertificates(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return certs
		}
		if block.Type != pemTypeCertificate {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certs = append(certs, cert)
	}
}

func encodeCertificates(certs []*x509.Certificate) []byte {
	var buffer bytes.Buffer
	for _, cert := range certs {
		_ = pem.Encode(&buffer, &pem.Block{Type: pemTypeCertificate, Bytes: cert.Raw})
	}
	return buffer.Bytes()
}

// appendUnique appends the certificates that are not yet contained in the list.
func appendUnique(certs []*x509.Certificate, additional ...*x509.Certificate) []*x509.Certificate {
	for _, candidate := range additional {
		if !containsCertificate(certs, candidate) {
			certs = append(certs, candidate)
		}
	}
	return certs
}

func containsCertificate(certs []*x509.Certificate, candidate *x509.Certificate) bool {
	return slices.ContainsFunc(certs, func(cert *x509.Certificate) bool {
		return cert.Equal(candidate)
	})
}

func isOperatorCreated(cert *x509.Certificate) bool {
	return slices.Contains(cert.Subject.Organization, operatorMarkerOrganization)
}

func isLegacyChartCa(cert *x509.Certificate) bool {
	return cert.Subject.CommonName == legacyChartCaCommonName && !isOperatorCreated(cert)
}

func isExpired(cert *x509.Certificate, now time.Time) bool {
	return now.After(cert.NotAfter)
}

func withoutExpired(certs []*x509.Certificate, now time.Time) []*x509.Certificate {
	return slices.DeleteFunc(slices.Clone(certs), func(cert *x509.Certificate) bool {
		return isExpired(cert, now)
	})
}

// servingDnsNames returns the DNS names under which the API server reaches the webhook service.
func servingDnsNames(serviceName string, namespace string) []string {
	return []string{
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	}
}

func missingDnsNames(cert *x509.Certificate, dnsNames []string) []string {
	var missing []string
	for _, dnsName := range dnsNames {
		if !slices.Contains(cert.DNSNames, dnsName) {
			missing = append(missing, dnsName)
		}
	}
	return missing
}

// verifyServingCertChain checks that the serving certificate chains up to the CA and has not expired. A NotBefore in the
// future is accepted, so that replicas with a clock skew do not discard material that another replica has just created.
// The DNS names are checked separately, see missingDnsNames.
func verifyServingCertChain(serving *x509.Certificate, ca *x509.Certificate, now time.Time) error {
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, err := serving.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: latest(now, serving.NotBefore, ca.NotBefore),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}

func latest(times ...time.Time) time.Time {
	result := times[0]
	for _, t := range times[1:] {
		if t.After(result) {
			result = t
		}
	}
	return result
}
