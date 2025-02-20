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

const (
	tokenUpdateServiceUrlEnvVarName = "TOKEN_UPDATE_SERVICE_URL"
	tokenEnvVarName                 = "SELF_MONITORING_AND_API_AUTH_TOKEN"
	certDir                         = "/tmp/k8s-webhook-server/serving-certs"
)

var (
	caCertPath = fmt.Sprintf("%s/ca.crt", certDir)
	tlsCert    = fmt.Sprintf("%s/tls.crt", certDir)
	tlsKey     = fmt.Sprintf("%s/tls.key", certDir)
)

func main() {
	log.SetOutput(os.Stdout)
	log.Println("starting secret ref satellite")

	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	tokenUpdateServiceUrl := os.Getenv(tokenUpdateServiceUrlEnvVarName)
	if tokenUpdateServiceUrl == "" {
		log.Fatalf("missing environment variable: %s, exiting", tokenUpdateServiceUrlEnvVarName)
	}
	tokenUpdateServiceEndpoint := fmt.Sprintf("%s/update-auth-token", tokenUpdateServiceUrl)

	httpClient := createHttpClient()

	// We only read the token env var once, at startup. It is either set or not. If it is not set, the only way to set
	// it is that the operator manager modifies the secret ref satellite deployment by adding the env var with the
	// secret as the env var source. This will restart the secret ref satellite pod and this container.
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

	log.Println("stopping secret ref satellite")
}

func createHttpClient() *http.Client {
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
		ServerName:   "dash0-operator-token-update.dash0-system.svc",
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
			Factor:   1.5,
			Steps:    5,
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
		"unexpected status code %d when updating the token, response body is %s",
		res.StatusCode,
		string(responseBody),
	)
	return statusCodeErr
}
