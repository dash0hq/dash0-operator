// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	promconfig "github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, name string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue returns a certificate and key in PEM format, signed by the CA.
func (ca *testCA) issue(t *testing.T, name string, usage x509.ExtKeyUsage) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// writeServerFiles writes the files of the server's secret volume: the CA for client certificates and the server
// certificate.
func writeServerFiles(t *testing.T, files *config.TLS, clientCA *testCA, serverCA *testCA) {
	t.Helper()
	certPEM, keyPEM := serverCA.issue(t, "scrape-coordinator", x509.ExtKeyUsageServerAuth)
	require.NoError(t, os.WriteFile(files.CAFile, clientCA.pem, 0o600))
	require.NoError(t, os.WriteFile(files.CertFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(files.KeyFile, keyPEM, 0o600))
}

type tlsTestSetup struct {
	server   *Server
	files    *config.TLS
	serverCA *testCA
	url      string
}

func startTLSServer(t *testing.T, clientCA *testCA) *tlsTestSetup {
	t.Helper()
	dir := t.TempDir()
	files := &config.TLS{
		ListenAddr: ":0",
		CAFile:     filepath.Join(dir, "ca.crt"),
		CertFile:   filepath.Join(dir, "tls.crt"),
		KeyFile:    filepath.Join(dir, "tls.key"),
	}
	serverCA := newTestCA(t, "server-ca")
	writeServerFiles(t, files, clientCA, serverCA)

	s := newTestServer(t, newAllocator(), Options{ListenAddr: ":0", TLS: files})
	require.NoError(t, s.SetScrapeConfigs([]*promconfig.ScrapeConfig{basicAuthScrapeConfig(t, "job")}))
	httpsServer := httptest.NewUnstartedServer(s.tlsServer.Handler)
	httpsServer.TLS = s.tlsServer.TLSConfig
	httpsServer.StartTLS()
	t.Cleanup(httpsServer.Close)
	return &tlsTestSetup{server: s, files: files, serverCA: serverCA, url: httpsServer.URL}
}

func (setup *tlsTestSetup) client(t *testing.T, clientCA *testCA) *http.Client {
	t.Helper()
	rootCAs := x509.NewCertPool()
	rootCAs.AppendCertsFromPEM(setup.serverCA.pem)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs}
	if clientCA != nil {
		certPEM, keyPEM := clientCA.issue(t, "collector", x509.ExtKeyUsageClientAuth)
		certificate, err := tls.X509KeyPair(certPEM, keyPEM)
		require.NoError(t, err)
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}

func (setup *tlsTestSetup) getScrapeConfigs(t *testing.T, client *http.Client) (*http.Response, error) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, setup.url+"/scrape_configs", http.NoBody)
	require.NoError(t, err)
	response, err := client.Do(request)
	if err == nil {
		t.Cleanup(func() { _ = response.Body.Close() })
	}
	return response, err
}

func TestTLSListenerServesUnmaskedScrapeConfigs(t *testing.T) {
	clientCA := newTestCA(t, "client-ca")
	setup := startTLSServer(t, clientCA)

	response, err := setup.getScrapeConfigs(t, setup.client(t, clientCA))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var scrapeConfigs map[string]map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&scrapeConfigs))
	assert.Equal(t, "secret-password", basicAuthPassword(scrapeConfigs["job"]))
}

func TestTLSListenerRejectsClientsWithoutAValidCertificate(t *testing.T) {
	clientCA := newTestCA(t, "client-ca")
	setup := startTLSServer(t, clientCA)

	_, err := setup.getScrapeConfigs(t, setup.client(t, nil))
	require.Error(t, err, "a client without a certificate is rejected")

	_, err = setup.getScrapeConfigs(t, setup.client(t, newTestCA(t, "other-ca")))
	require.Error(t, err, "a client with a certificate of another CA is rejected")
}

func TestPlainListenerOnlyServesHealthEndpointsWithTLS(t *testing.T) {
	setup := startTLSServer(t, newTestCA(t, "client-ca"))
	handler := setup.server.plainServer.Handler
	assert.Equal(t, http.StatusOK, get(t, handler, "/livez").Code)
	assert.Equal(t, http.StatusOK, get(t, handler, "/readyz").Code)
	assert.Equal(t, http.StatusNotFound, get(t, handler, "/scrape_configs").Code)
	assert.Equal(t, http.StatusNotFound, get(t, handler, "/jobs/job/targets?collector_id="+testCollector).Code)
}

func TestCertificateAndCARotation(t *testing.T) {
	oldClientCA := newTestCA(t, "old-client-ca")
	setup := startTLSServer(t, oldClientCA)
	oldServerCA := setup.serverCA

	newClientCA := newTestCA(t, "new-client-ca")
	newServerCA := newTestCA(t, "new-server-ca")
	writeServerFiles(t, setup.files, newClientCA, newServerCA)
	require.NoError(t, setup.server.certWatcher.ReadCertificate())

	setup.serverCA = newServerCA
	require.Eventually(t, func() bool {
		response, err := setup.getScrapeConfigs(t, setup.client(t, newClientCA))
		return err == nil && response.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "the new server certificate and the new client CA are used")

	setup.serverCA = oldServerCA
	_, err := setup.getScrapeConfigs(t, setup.client(t, oldClientCA))
	require.Error(t, err, "the old certificates are no longer accepted")
}

func TestInvalidCAFails(t *testing.T) {
	dir := t.TempDir()
	files := &config.TLS{
		ListenAddr: ":0",
		CAFile:     filepath.Join(dir, "ca.crt"),
		CertFile:   filepath.Join(dir, "tls.crt"),
		KeyFile:    filepath.Join(dir, "tls.key"),
	}
	writeServerFiles(t, files, newTestCA(t, "client-ca"), newTestCA(t, "server-ca"))
	require.NoError(t, os.WriteFile(files.CAFile, []byte("not a certificate"), 0o600))

	_, err := New(discardLogger, newAllocator(), Options{ListenAddr: ":0", TLS: files})
	require.Error(t, err)
}
