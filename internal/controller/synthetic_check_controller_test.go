// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	syntheticCheckName        = "test-synthetic-check"
	syntheticCheckName2       = "test-synthetic-check-2"
	syntheticCheckApiBasePath = "/api/synthetic-checks/"
)

var (
	defaultExpectedPathSyntheticCheck  = fmt.Sprintf("%s.*%s", syntheticCheckApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-synthetic-check")
	defaultExpectedPathSyntheticCheck2 = fmt.Sprintf("%s.*%s", syntheticCheckApiBasePath, "dash0-operator_.*_test-dataset_test-namespace-2_test-synthetic-check-2")
	leaderElectionAware                = leaderElectionAwareMock{
		isLeader: true,
	}
)

type leaderElectionAwareMock struct {
	isLeader bool
}

func (l *leaderElectionAwareMock) NeedLeaderElection() bool {
	return true
}

func (l *leaderElectionAwareMock) IsLeader() bool {
	return l.isLeader
}

var _ = Describe("The Synthetic Check controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var testStartedAt time.Time

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	AfterAll(func() {
		DeleteNamespace(ctx, k8sClient, TestNamespaceName2)
	})

	BeforeEach(func() {
		testStartedAt = time.Now()
		extraMonitoringResourceNames = make([]types.NamespacedName, 0)
	})

	AfterEach(func() {
		DeleteMonitoringResource(ctx, k8sClient)
		for _, name := range extraMonitoringResourceNames {
			DeleteMonitoringResourceByName(ctx, k8sClient, name, true)
		}
		extraMonitoringResourceNames = make([]types.NamespacedName, 0)
	})

	Describe("the synthetic check reconciler", func() {
		var syntheticCheckReconciler *SyntheticCheckReconciler

		BeforeEach(func() {
			syntheticCheckReconciler = createSyntheticCheckReconciler()

			syntheticCheckReconciler.apiConfig.Store(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			})
			syntheticCheckReconciler.authToken.Store(&AuthorizationTokenTest)

			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			syntheticCheckReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
			deleteSyntheticCheckResourceIfItExists(ctx, k8sClient, TestNamespaceName, syntheticCheckName)
			deleteSyntheticCheckResourceIfItExists(ctx, k8sClient, TestNamespaceName2, syntheticCheckName2)
		})

		It("it ignores synthetic check resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			Expect(gock.IsPending()).To(BeTrue())
			verifySyntheticCheckHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
			)
		})

		It("it ignores synthetic check resource changes if the API endpoint is not configured", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			syntheticCheckReconciler.RemoveApiEndpointAndDataset(ctx, &logger)

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsPending()).To(BeTrue())
			verifySyntheticCheckHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
			)
		})

		It("it ignores synthetic check resource changes if the auth token endpoint has not been provided yet", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckPutRequest(defaultExpectedPathDashboard)
			defer gock.Off()

			syntheticCheckReconciler.RemoveAuthToken(ctx, &logger)

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsPending()).To(BeTrue())
			verifySyntheticCheckHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
			)
		})

		It("creates a synthetic check", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a synthetic check", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			// Modify the resource
			syntheticCheckResource.Spec.Display.Name = "Updated Synthetic Check"
			Expect(k8sClient.Update(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a synthetic check", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckDeleteRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())
			Expect(k8sClient.Delete(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a synthetic check if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckDeleteRequestWithHttpStatus(defaultExpectedPathSyntheticCheck, http.StatusNotFound)
			defer gock.Off()

			syntheticCheckResource :=
				createSyntheticCheckResourceWithEnableLabel(TestNamespaceName, syntheticCheckName, "false")
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("creates a synthetic check if dash0.com/enable is set but not to \"false\"", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource :=
				createSyntheticCheckResourceWithEnableLabel(TestNamespaceName, syntheticCheckName, "whatever")
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports http errors when synchronizing a synthetic check", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathSyntheticCheck).
				MatchParam("dataset", DatasetCustomTest).
				Times(3).
				Reply(503).
				JSON(map[string]string{})
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusFailed,
				testStartedAt,
				"unexpected status code 503 when synchronizing the check \"test-synthetic-check\": "+
					"PUT https://api.dash0.com/api/synthetic-checks/"+
					"dash0-operator_cluster-uid-test_test-dataset_test-namespace_test-synthetic-check?dataset=test-dataset, response body is {}",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("retries synchronization when synchronizing a synthetic check", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathSyntheticCheck).
				MatchParam("dataset", DatasetCustomTest).
				Times(2).
				Reply(503).
				JSON(map[string]string{})
			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			defer gock.Off()

			syntheticCheckResource := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource)).To(Succeed())

			result, err := syntheticCheckReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      syntheticCheckName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		type maybeDoInitialSynchronizationOfAllResourcesTest struct {
			disableSync func()
			enabledSync func()
		}

		DescribeTable("synchronizes all existing synthetic check resources when the auth token or api endpoint become available", func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			// Disable synchronization by removing the auth token or api endpoint.
			testConfig.disableSync()

			EnsureNamespaceExists(ctx, k8sClient, TestNamespaceName2)
			secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
				ctx,
				k8sClient,
				MonitoringResourceDefaultSpec,
				types.NamespacedName{Namespace: TestNamespaceName2, Name: MonitoringResourceName},
			)
			extraMonitoringResourceNames = append(extraMonitoringResourceNames, types.NamespacedName{
				Namespace: secondMonitoringResource.Namespace,
				Name:      secondMonitoringResource.Name,
			})

			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck)
			expectSyntheticCheckPutRequest(defaultExpectedPathSyntheticCheck2)
			defer gock.Off()

			syntheticCheckResource1 := createSyntheticCheckResource(TestNamespaceName, syntheticCheckName)
			Expect(k8sClient.Create(ctx, syntheticCheckResource1)).To(Succeed())
			syntheticCheckResource2 := createSyntheticCheckResource(TestNamespaceName2, syntheticCheckName2)
			Expect(k8sClient.Create(ctx, syntheticCheckResource2)).To(Succeed())

			// verify that the synthetic checks have not been synchronized yet
			Expect(gock.IsPending()).To(BeTrue())
			verifySyntheticCheckHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, syntheticCheckName)
			verifySyntheticCheckHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName2, syntheticCheckName2)

			// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
			// synchronization of all resources.
			testConfig.enabledSync()

			// Verify both synthetic check resources have been synchronized
			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				syntheticCheckName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			verifySyntheticCheckSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName2,
				syntheticCheckName2,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		},
			Entry("when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					syntheticCheckReconciler.RemoveAuthToken(ctx, &logger)
				},
				enabledSync: func() {
					syntheticCheckReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
				},
			}),
			Entry("when the api endpoint becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					syntheticCheckReconciler.RemoveApiEndpointAndDataset(ctx, &logger)
				},
				enabledSync: func() {
					syntheticCheckReconciler.SetApiEndpointAndDataset(
						ctx,
						&ApiConfig{
							Endpoint: ApiEndpointTest,
							Dataset:  DatasetCustomTest,
						},
						&logger)
				},
			}),
			Entry("when the operator manager becomes leader", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					leaderElectionAware.isLeader = false
				},
				enabledSync: func() {
					leaderElectionAware.isLeader = true
					syntheticCheckReconciler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				},
			}),
			Entry("only sync once evenn if the the auth token is set multiple times", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					syntheticCheckReconciler.RemoveAuthToken(ctx, &logger)
				},
				enabledSync: func() {
					// gock only expects two PUT requests, so if we would synchronize twice, the test would fail
					syntheticCheckReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
					syntheticCheckReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
				},
			}),
		)
	})
})

func createSyntheticCheckReconciler() *SyntheticCheckReconciler {
	syntheticCheckReconciler := NewSyntheticCheckReconciler(
		k8sClient,
		ClusterUidTest,
		&leaderElectionAware,
		&http.Client{},
	)
	return syntheticCheckReconciler
}

func expectSyntheticCheckPutRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(200).
		JSON(map[string]string{})
}

func expectSyntheticCheckDeleteRequest(expectedPath string) {
	expectSyntheticCheckDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectSyntheticCheckDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status).
		JSON(map[string]string{})
}

func createSyntheticCheckResource(namespace string, name string) *dash0v1alpha1.Dash0SyntheticCheck {
	return createSyntheticCheckResourceWithEnableLabel(namespace, name, "")
}

func createSyntheticCheckResourceWithEnableLabel(namespace string, name string, dash0EnableLabelValue string) *dash0v1alpha1.Dash0SyntheticCheck {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0SyntheticCheck{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0SyntheticCheck",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0SyntheticCheckSpec{
			Display: dash0v1alpha1.Dash0SyntheticCheckDisplay{
				Name: "Test Synthetic Check",
			},
			Plugin: dash0v1alpha1.Dash0SyntheticCheckPlugin{
				Kind: "http",
				Spec: dash0v1alpha1.Dash0SyntheticCheckHTTPPluginSpec{
					Request: dash0v1alpha1.Dash0SyntheticCheckHTTPRequest{
						Method:    "get",
						URL:       "https://example.com",
						Redirects: "follow",
						TLS: dash0v1alpha1.Dash0SyntheticCheckHTTPTLS{
							AllowInsecure: false,
						},
						Tracing: dash0v1alpha1.Dash0SyntheticCheckHTTPTracing{
							AddTracingHeaders: false,
						},
						Headers:         []dash0v1alpha1.Dash0SyntheticCheckHTTPHeader{},
						QueryParameters: []dash0v1alpha1.Dash0SyntheticCheckHTTPQueryParameter{},
					},
					Assertions: dash0v1alpha1.Dash0SyntheticCheckHTTPAssertions{
						CriticalAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{
							{
								Kind: "status_code",
								Spec: dash0v1alpha1.Dash0SyntheticCheckAssertionSpec{
									Operator: ptr.To("is"),
									Value:    ptr.To("200"),
								},
							},
						},
						DegradedAssertions: []dash0v1alpha1.Dash0SyntheticCheckAssertion{},
					},
				},
			},
			Schedule: dash0v1alpha1.Dash0SyntheticCheckSchedule{
				Strategy:  "all_locations",
				Interval:  "5m",
				Locations: []string{"us-east-1"},
			},
			Retries: dash0v1alpha1.Dash0SyntheticCheckRetries{
				Kind: "off",
				Spec: dash0v1alpha1.Dash0SyntheticCheckRetriesSpec{},
			},
			Notifications: dash0v1alpha1.Dash0SyntheticCheckNotifications{
				Channels: []dash0v1alpha1.NotificationChannelID{},
			},
			Enabled: true,
		},
	}
}

func deleteSyntheticCheckResourceIfItExists(ctx context.Context, k8sClient client.Client, namespace string, name string) {
	syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(ctx, syntheticCheck, &client.DeleteOptions{
		GracePeriodSeconds: new(int64),
	})
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifySyntheticCheckSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedError string,
) {
	Eventually(func(g Gomega) {
		syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, syntheticCheck)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(syntheticCheck.Status.SynchronizationStatus).To(Equal(expectedStatus))
		g.Expect(syntheticCheck.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
		g.Expect(syntheticCheck.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
		g.Expect(syntheticCheck.Status.SynchronizationError).To(ContainSubstring(expectedError))
		// synthetic checks have no operator-side validations, all local validations are encoded in the CRD already
		g.Expect(syntheticCheck.Status.ValidationIssues).To(BeNil())
	}).Should(Succeed())
}

func verifySyntheticCheckHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(func(g Gomega) {
		syntheticCheck := &dash0v1alpha1.Dash0SyntheticCheck{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, syntheticCheck)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(syntheticCheck.Status.SynchronizationStatus)).To(Equal(""))
		g.Expect(syntheticCheck.Status.SynchronizationError).To(Equal(""))
		g.Expect(syntheticCheck.Status.ValidationIssues).To(BeNil())
	}).Should(Succeed())
}
