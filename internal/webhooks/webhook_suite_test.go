// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	manager   ctrl.Manager
	cfg       *rest.Config
	k8sClient client.Client
	clientset *kubernetes.Clientset
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc

	operatorConfigurationMutatingWebhookHandler *OperatorConfigurationMutatingWebhookHandler
	monitoringMutatingWebhookHandler            *MonitoringMutatingWebhookHandler
)

func TestWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Suite")
}

var _ = BeforeSuite(func() {
	format.MaxLength = 0

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	var err error
	scheme := apimachineryruntime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(dash0v1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(dash0v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(admissionv1.AddToScheme(scheme)).To(Succeed())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		CRDInstallOptions: envtest.CRDInstallOptions{
			// Note: passing the scheme with our CRDs to envtest is required for testing conversion webhooks.
			Scheme: scheme,
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", EnvtestK8sVersion, runtime.GOOS, runtime.GOARCH)),
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}
	Expect(testEnv).NotTo(BeNil())

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(clientset).NotTo(BeNil())

	By("setting up resources")
	setupTestResources()

	// start webhook server using Manager
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	manager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookInstallOptions.LocalServingHost,
			Port:    webhookInstallOptions.LocalServingPort,
			CertDir: webhookInstallOptions.LocalServingCertDir,
		}),
		LeaderElection: false,
		Metrics:        metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	// Note: When adding new webhooks, make sure to also add them to the Kustomize configs in
	// config/webhook/manifests.yaml, not only to the Helm chart. While the Helm chart is used for registering webhooks
	// in an actual production deployment of the operator, the Kustomize configs are still used for registering
	// webhooks in the unit tests here; via testEnv = &envtest.Environment.WebhookInstallOptions.Paths

	Expect(NewInstrumentationWebhookHandler(
		k8sClient,
		manager.GetEventRecorderFor("dash0-webhook"),
		util.NewClusterInstrumentationConfig(
			TestImages,
			OTelCollectorNodeLocalBaseUrlTest,
			util.ExtraConfigDefaults,
			nil,
			false,
		),
	).SetupWebhookWithManager(manager)).To(Succeed())

	operatorConfigurationMutatingWebhookHandler = NewOperatorConfigurationMutatingWebhookHandler(k8sClient)
	Expect(operatorConfigurationMutatingWebhookHandler.SetupWebhookWithManager(manager)).To(Succeed())

	Expect(NewOperatorConfigurationValidationWebhookHandler(k8sClient).SetupWebhookWithManager(manager)).To(Succeed())

	Expect(SetupDash0MonitoringConversionWebhookWithManager(manager)).To(Succeed())

	monitoringMutatingWebhookHandler = NewMonitoringMutatingWebhookHandler(k8sClient, OperatorNamespace)
	Expect(monitoringMutatingWebhookHandler.SetupWebhookWithManager(manager)).To(Succeed())

	Expect(NewMonitoringValidationWebhookHandler(k8sClient).SetupWebhookWithManager(manager)).To(Succeed())

	//+kubebuilder:scaffold:webhook

	go func() {
		defer GinkgoRecover()
		err = manager.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		return conn.Close()
	}).Should(Succeed())
})

func setupTestResources() {
	EnsureTestNamespaceExists(ctx, k8sClient)
}

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
