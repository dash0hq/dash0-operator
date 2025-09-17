// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postinstall

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

var (
	k8sClient          client.Client
	clientset          *kubernetes.Clientset
	postInstallHandler *OperatorPostInstallHandler
	cfg                *rest.Config
	testEnv            *envtest.Environment

	postInstallHandlerRetryBackoffForTests = wait.Backoff{
		Duration: 50 * time.Millisecond,
		Factor:   1,
		Steps:    4,
	}
	expectedPostInstallHandlerTimeoutNanosecondsMin = 150_000_000
	// with the backoff settings above, all negative test cases should time out in ~200ms. For some reason, the test
	// "operator configuration resource has been created but is not available" takes around 2 seconds. However, the
	// exact timeout is not critical, as long as we can verify that
	// WaitForOperatorConfigurationResourceToBecomeAvailable times out.
	expectedPostInstallHandlerTimeoutNanosecondsMax = 5_000_000_000
)

func TestOperatorConfigurationAvailableCheck(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Post-Install Suite")
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
	Expect(dash0v1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	postInstallHandler, err = NewOperatorPostInstallHandlerFromConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	postInstallHandler.setRetryBackoff(postInstallHandlerRetryBackoffForTests)

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
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
