// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// The secret ref resolver is a small auxiliary component that is required for one specific use scenario: The Dash0
// auth token is provided via a Kubernetes secret, and self-monitoring is enabled. The way to resolve Kubernetes secrets
// is to either mount them as a volume or as an environment variable. For components that the operator manager manages,
// this is not an issue: It can just add the secret as an environment variable to them, for example to the various
// containers of the OpenTelemetry collector daemonset & deployment. But for sending its own self-monitoring telemetry,
// the operator manager process needs to resolve the Kubernetes secret to an auth token as well. It could add the secret
// to its own Kubernetes deployment, but that would trigger a restart of the operator manager, which is bad for several
// reasons:
//   - When the operator configuration resource is deployed automatically via Helm, and then users later tries to modify
//     it in any way, or delete it, the following happens: A reconcile for the changed/deleted operator configuration will
//     be triggered, this reconcile will set the self-monitoring/API access env vars on the operator manager
//     deployment (on itself), and the deployment will be updated via the K8s client. This will lead to a restart of the
//     operator manager process when starting up again, but the operator manager will be started with the same command
//     line parameters (the ones determined by the Helm values that were originally used when doing Helm install), which
//     in turn will recreate the deleted operator configuration resource or overwrite the changed values. This leads to
//     effectively ignoring the changes the user made to the resource.
//   - Another issue is that a restart leads to longer operator manager startup times. The first start of the operator
//     manager is relatively quick, it then gets a leader election lease and then would get restarted shortly after that
//     (after adding the Kubernetes secret env var). When the changed pod comes up after the auto-restart, the old one is
//     not yet terminated (due to how rolling updates work for K8s deployments), which means that the new pod needs to
//     wait for a longer time (often > 30 seconds) until it gets the leader election lease.
//   - Last but not least, the self-restart can happen at any time, in the middle of whatever the operator manager is
//     doing at the moment — reconciling custom resources, setting up the OTel collectors etc. This makes it harder to
//     reason about the operator manager behavior.
//
// Long story short, we need a way to resolve the Kubernetes secret without restarting the operator manager. Which is
// where the secret ref resolver comes in. Its only purpose is that the operator manager can add the secret to the
// secret ref resolver deployment, the secret resolver will restart and then notify the operator and hand over the
// resolved auth token.
//
// Security considerations: An obvious question is: Could this component be abused by a bad actor to get access to the
// content of a secret they are not supposed to have? The answer is no. An actor _without_ access to the cluster cannot
// do anything with a secret resolver deployed in the cluster. An actor _with_ access to the cluster could either deploy
// the secret resolver indepently of the operator and mount the secret they are interested plus setting the
// TOKEN_UPDATE_SERVICE_URL to an endpoint under their control; or they could also use the existing secret resolver
// deployment of the operator (assuming they have access to the dash0-system namespace), changing its
// TOKEN_UPDATE_SERVICE_URL to an endpoint under their control. But with that level of access they could do all of that
// without this component as well, for example by deploying a one-off pod with the secret mounted as an environment
// variable or volume and get access to the secret's content that way.
const (
	tokenUpdateServiceServerNameEnvVarName = "TOKEN_UPDATE_SERVICE_SERVER_NAME"
	tokenUpdateServiceUrlEnvVarName        = "TOKEN_UPDATE_SERVICE_URL"
	tokenEnvVarName                        = "SELF_MONITORING_AND_API_AUTH_TOKEN"
	certDir                                = "/tmp/k8s-webhook-server/serving-certs"
)

var (
	caCertPath = fmt.Sprintf("%s/ca.crt", certDir)
	tlsCert    = fmt.Sprintf("%s/tls.crt", certDir)
	tlsKey     = fmt.Sprintf("%s/tls.key", certDir)
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("starting secret ref resolver")

	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	tokenUpdateServiceServerName := os.Getenv(tokenUpdateServiceServerNameEnvVarName)
	if tokenUpdateServiceServerName == "" {
		log.Fatalf("missing environment variable: %s, exiting", tokenUpdateServiceServerNameEnvVarName)
	}
	tokenUpdateServiceUrl := os.Getenv(tokenUpdateServiceUrlEnvVarName)
	if tokenUpdateServiceUrl == "" {
		log.Fatalf("missing environment variable: %s, exiting", tokenUpdateServiceUrlEnvVarName)
	}
	tokenUpdateServiceEndpoint := fmt.Sprintf("%s/update-auth-token", tokenUpdateServiceUrl)

	httpClient := createHttpClient(tokenUpdateServiceServerName)

	// We only read the token env var once, at startup. It is either set or not. If it is not set, the only way to set
	// it is that the operator manager modifies the secret ref resolver deployment by adding the env var with the
	// secret as the env var source. This will restart the secret ref resolver pod and this container.
	tokenValue := os.Getenv(tokenEnvVarName)
	if tokenValue != "" {
		log.Println("read token value, will notify the operator manager")
		if err := notifyOperatorManagerWithRetry(httpClient, tokenUpdateServiceEndpoint, tokenValue); err != nil {
			log.Printf("notifying the operator manager failed after multiple retries, will not be retried again: %v\n", err)
		}
	} else {
		log.Println("token value empty, doing nothing")
	}

	// now just keep the process alive and idling
	go func() {
		for {
			receivedSignal := <-shutdown
			log.Printf("received signal: %s, stopping", receivedSignal)
			done <- true
		}
	}()

	log.Println("entering idle wait status")
	<-done

	log.Println("stopping secret ref resolver")
}

func createHttpClient(tokenUpdateServiceServerName string) *http.Client {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		log.Fatalf("error creating certificate pool: %v", err)
	}

	x509KeyPair, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	if err != nil {
		log.Fatalf(
			"Error loading x509 key pair from certificate %s and key file %s: %v",
			tlsCert,
			tlsKey,
			err,
		)
	}

	caCertContent, err := os.ReadFile(caCertPath)
	if err != nil {
		log.Fatalf("error reading CA cert %s: %v", caCertPath, err)
	}
	if ok := certPool.AppendCertsFromPEM(caCertContent); !ok {
		log.Fatalf("invalid cert in CA %s", caCertPath)
	}

	tlsConfig := &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{x509KeyPair},
		ServerName:   tokenUpdateServiceServerName,
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   400 * time.Millisecond,
	}
}

func notifyOperatorManagerWithRetry(
	httpClient *http.Client,
	tokenUpdateServiceEndpoint string,
	tokenValue string,
) error {
	return retry.OnError(
		wait.Backoff{
			Duration: 500 * time.Millisecond,
			Factor:   1.1,
			// Allow for a very generous retry duration with many attempts. (We effectively keep trying for > 10 minutes
			// here.) We update the secret resolver deployment directly at startup of the operator manager, if a Dash0
			// export with secret ref is specified for the dash0-operator-configuration-auto-resource via Helm values,
			// but the operator manager might need some time to also start the token update service web server.
			Steps: 1200,
		},
		func(err error) bool {
			log.Printf("notifying the operator manager failed, might be retried: %v\n", err)
			return true
		},
		func() error {
			return notifyOperatorManager(httpClient, tokenUpdateServiceEndpoint, tokenValue)
		},
	)
}

func notifyOperatorManager(httpClient *http.Client, tokenUpdateServiceEndpoint string, tokenValue string) error {
	log.Printf("calling POST %s\n", tokenUpdateServiceEndpoint)
	requestPayload := bytes.NewBufferString(tokenValue)
	req, err := http.NewRequest(
		http.MethodPost,
		tokenUpdateServiceEndpoint,
		requestPayload,
	)
	if err != nil {
		return fmt.Errorf("unable to create HTTP request: %w", err)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to execute HTTP request: %w", err)
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		// HTTP status is not 2xx, treat this as an error
		// convertNon2xxStatusCodeToError will also consume and close the response body
		statusCodeError := convertNon2xxStatusCodeToError(res)
		return statusCodeError
	}

	log.Printf("call to POST %s was successful (HTTP %d)\n", tokenUpdateServiceEndpoint, res.StatusCode)

	// HTTP status code was 2xx, discard the response body and close it
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()
	return nil
}

func convertNon2xxStatusCodeToError(res *http.Response) error {
	defer func() {
		_ = res.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the HTTP response payload after receiving status code %d when "+
			"trying to update the token",
			res.StatusCode,
		)
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when updating the token, response body is \"%s\"",
		res.StatusCode,
		string(responseBody),
	)
	return statusCodeErr
}
