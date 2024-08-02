package removal

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/dash0/controller"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
	testutil "github.com/dash0hq/dash0-operator/test/util"
)

const (
	preDeleteHandlerTimeoutForTests = 5 * time.Second
)

var (
	k8sClient        client.Client
	clientset        *kubernetes.Clientset
	preDeleteHandler *OperatorPreDeleteHandler
	reconciler       *controller.Dash0Reconciler
	cfg              *rest.Config
	testEnv          *envtest.Environment

	images = util.Images{
		OperatorImage:                "some-registry.com:1234/dash0hq/operator-controller:1.2.3",
		InitContainerImage:           "some-registry.com:1234/dash0hq/instrumentation:4.5.6",
		InitContainerImagePullPolicy: corev1.PullAlways,
	}
)

func TestRemoval(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Removal Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
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

	oTelColResourceManager := &otelcolresources.OTelColResourceManager{
		Client:                  k8sClient,
		OTelCollectorNamePrefix: "unit-test",
	}
	backendConnectionManager := &backendconnection.BackendConnectionManager{
		Client:                 k8sClient,
		Clientset:              clientset,
		Scheme:                 k8sClient.Scheme(),
		OTelColResourceManager: oTelColResourceManager,
	}
	reconciler = &controller.Dash0Reconciler{
		Client:                   k8sClient,
		Clientset:                clientset,
		Recorder:                 mgr.GetEventRecorderFor("dash0-controller"),
		Scheme:                   k8sClient.Scheme(),
		Images:                   images,
		OTelCollectorBaseUrl:     "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318",
		OperatorNamespace:        testutil.Dash0OperatorNamespace,
		BackendConnectionManager: backendConnectionManager,
	}
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
