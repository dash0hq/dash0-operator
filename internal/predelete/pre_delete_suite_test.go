// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package predelete

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	preDeleteHandlerTimeoutForTests = 5 * time.Second
)

var (
	k8sClient        client.Client
	clientset        *kubernetes.Clientset
	preDeleteHandler *OperatorPreDeleteHandler
	reconciler       *controller.MonitoringReconciler
	cfg              *rest.Config
	testEnv          *envtest.Environment
)

func TestRemoval(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Pre-Delete Suite")
}

var _ = BeforeSuite(func() {
	format.MaxLength = 0

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.28.3-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(dash0v1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	preDeleteHandler, err = NewOperatorPreDeleteHandlerFromConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	preDeleteHandler.SetTimeout(preDeleteHandlerTimeoutForTests)

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(clientset).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(mgr).NotTo(BeNil())

	instrumenter := instrumentation.NewInstrumenter(
		k8sClient,
		clientset,
		mgr.GetEventRecorderFor("dash0-monitoring-controller"),
		TestImages,
		util.ExtraConfigDefaults,
		OTelCollectorBaseUrlTest,
		false,
		nil,
	)
	oTelColResourceManager := &otelcolresources.OTelColResourceManager{
		Client:                    k8sClient,
		Scheme:                    k8sClient.Scheme(),
		OperatorManagerDeployment: OperatorManagerDeployment,
		OTelCollectorNamePrefix:   OTelCollectorNamePrefixTest,
		ExtraConfig:               util.ExtraConfigDefaults,
	}
	collectorManager := &collectors.CollectorManager{
		Client:                 k8sClient,
		Clientset:              clientset,
		OTelColResourceManager: oTelColResourceManager,
	}
	reconciler = &controller.MonitoringReconciler{
		Client:                 k8sClient,
		Clientset:              clientset,
		Images:                 TestImages,
		Instrumenter:           instrumenter,
		OperatorNamespace:      OperatorNamespace,
		CollectorManager:       collectorManager,
		DanglingEventsTimeouts: &DanglingEventsTimeoutsTest,
	}
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
