// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	viewName            = "test-view"
	extraNamespaceViews = "extra-namespace-views"
	viewName2           = "test-view-2"
	viewApiBasePath     = "/api/views/"

	viewId     = "view-id"
	viewOrigin = "view-origin"
)

var (
	defaultExpectedPathView  = fmt.Sprintf("%s.*%s", viewApiBasePath, "dash0-operator_.*_test-dataset_test-namespace_test-view")
	defaultExpectedPathView2 = fmt.Sprintf("%s.*%s", viewApiBasePath, "dash0-operator_.*_test-dataset_extra-namespace-views_test-view-2")
	viewLeaderElectionAware  = NewLeaderElectionAwareMock(true)

	viewResponseWithIdOriginDataset = map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"dash0.com/id":      viewId,
				"dash0.com/origin":  viewOrigin,
				"dash0.com/dataset": DatasetCustomTest,
			},
		},
	}
)

var _ = Describe("The View controller", Ordered, func() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	var testStartedAt time.Time

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureOperatorNamespaceExists(ctx, k8sClient)
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

	Describe("the view reconciler", func() {
		var viewReconciler *ViewReconciler

		BeforeEach(func() {
			viewReconciler = createViewReconciler()

			viewReconciler.apiConfig.Store(&ApiConfig{
				Endpoint: ApiEndpointTest,
				Dataset:  DatasetCustomTest,
			})
			viewReconciler.authToken.Store(&AuthorizationTokenTest)

			// to make tests that involve http retries faster, we do not want to wait for one second for each retry
			viewReconciler.overrideHttpRetryDelay(20 * time.Millisecond)
		})

		AfterEach(func() {
			DeleteMonitoringResourceIfItExists(ctx, k8sClient)
			deleteViewResourceIfItExists(ctx, k8sClient, TestNamespaceName, viewName)
			deleteViewResourceIfItExists(ctx, k8sClient, extraNamespaceViews, viewName2)
		})

		It("it ignores view resource changes if no Dash0 monitoring resource exists in the namespace", func() {
			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			Expect(gock.IsPending()).To(BeTrue())
			verifyViewHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
			)
		})

		It("it ignores view resource changes if the API endpoint is not configured", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewReconciler.RemoveApiEndpointAndDataset(ctx, &logger)

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsPending()).To(BeTrue())
			verifyViewHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
			)
		})

		It("it ignores view resource changes if the auth token endpoint has not been provided yet", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewReconciler.RemoveAuthToken(ctx, &logger)

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsPending()).To(BeTrue())
			verifyViewHasNoSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
			)
		})

		It("creates a view", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("updates a view", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			// Modify the resource
			viewResource.Spec.Display.Name = "Updated View"
			Expect(k8sClient.Update(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a view", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewDeleteRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())
			Expect(k8sClient.Delete(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(gock.IsDone()).To(BeTrue())
		})

		It("deletes a view if labelled with dash0.com/enable=false", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewDeleteRequestWithHttpStatus(defaultExpectedPathView, http.StatusNotFound)
			defer gock.Off()

			viewResource := createViewResourceWithEnableLabel(TestNamespaceName, viewName, "false")
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				false,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("creates a view if dash0.com/enable is set but not to \"false\"", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResourceWithEnableLabel(TestNamespaceName, viewName, "whatever")
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("reports http errors when synchronizing a view", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathView).
				MatchParam("dataset", DatasetCustomTest).
				Times(3).
				Reply(503).
				JSON(map[string]string{})
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusFailed,
				testStartedAt,
				false,
				"unexpected status code 503 when trying to synchronize the view \"test-view\": "+
					"PUT https://api.dash0.com/api/views/"+
					"dash0-operator_cluster-uid-test_test-dataset_test-namespace_test-view?dataset=test-dataset, response body is {}",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		It("retries synchronization when synchronizing a view", func() {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			gock.New(ApiEndpointTest).
				Put(defaultExpectedPathView).
				MatchParam("dataset", DatasetCustomTest).
				Times(2).
				Reply(503).
				JSON(viewResponseWithIdOriginDataset)
			expectViewPutRequest(defaultExpectedPathView)
			defer gock.Off()

			viewResource := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource)).To(Succeed())

			result, err := viewReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: TestNamespaceName,
					Name:      viewName,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		})

		type maybeDoInitialSynchronizationOfAllResourcesTest struct {
			disableSync func()
			enabledSync func()
		}

		DescribeTable("synchronizes all existing view resources when the auth token or api endpoint become available", func(testConfig maybeDoInitialSynchronizationOfAllResourcesTest) {
			EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)

			// Disable synchronization by removing the auth token or api endpoint.
			testConfig.disableSync()

			EnsureNamespaceExists(ctx, k8sClient, extraNamespaceViews)
			secondMonitoringResource := EnsureMonitoringResourceWithSpecExistsInNamespaceAndIsAvailable(
				ctx,
				k8sClient,
				MonitoringResourceDefaultSpec,
				types.NamespacedName{Namespace: extraNamespaceViews, Name: MonitoringResourceName},
			)
			extraMonitoringResourceNames = append(extraMonitoringResourceNames, types.NamespacedName{
				Namespace: secondMonitoringResource.Namespace,
				Name:      secondMonitoringResource.Name,
			})

			expectViewPutRequest(defaultExpectedPathView)
			expectViewPutRequest(defaultExpectedPathView2)
			defer gock.Off()

			viewResource1 := createViewResource(TestNamespaceName, viewName)
			Expect(k8sClient.Create(ctx, viewResource1)).To(Succeed())
			viewResource2 := createViewResource(extraNamespaceViews, viewName2)
			Expect(k8sClient.Create(ctx, viewResource2)).To(Succeed())

			// verify that the views have not been synchronized yet
			Expect(gock.IsPending()).To(BeTrue())
			verifyViewHasNoSynchronizationStatus(ctx, k8sClient, TestNamespaceName, viewName)
			verifyViewHasNoSynchronizationStatus(ctx, k8sClient, extraNamespaceViews, viewName2)

			// Now provide the auth token or API endpoint, which was unset before. This should trigger initial
			// synchronization of all resources.
			testConfig.enabledSync()

			// Verify both view resources have been synchronized
			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				TestNamespaceName,
				viewName,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			verifyViewSynchronizationStatus(
				ctx,
				k8sClient,
				extraNamespaceViews,
				viewName2,
				dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
				testStartedAt,
				true,
				"",
			)
			Expect(gock.IsDone()).To(BeTrue())
		},
			Entry("when the auth token becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					viewReconciler.RemoveAuthToken(ctx, &logger)
				},
				enabledSync: func() {
					viewReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
				},
			}),
			Entry("when the api endpoint becomes available", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					viewReconciler.RemoveApiEndpointAndDataset(ctx, &logger)
				},
				enabledSync: func() {
					viewReconciler.SetApiEndpointAndDataset(
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
					viewLeaderElectionAware.SetLeader(false)
				},
				enabledSync: func() {
					viewLeaderElectionAware.SetLeader(true)
					viewReconciler.NotifiyOperatorManagerJustBecameLeader(ctx, &logger)
				},
			}),
			Entry("only sync once evenn if the the auth token is set multiple times", maybeDoInitialSynchronizationOfAllResourcesTest{
				disableSync: func() {
					viewReconciler.RemoveAuthToken(ctx, &logger)
				},
				enabledSync: func() {
					// gock only expects two PUT requests, so if we would synchronize twice, the test would fail
					viewReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
					viewReconciler.SetAuthToken(ctx, AuthorizationTokenTest, &logger)
				},
			}),
		)
	})

	Describe("mapping view resources to http requests", func() {

		type viewToRequestTestConfig struct {
			view                string
			expectedAnnotations map[string]string
		}

		var viewReconciler *ViewReconciler

		BeforeEach(func() {
			viewReconciler = &ViewReconciler{}
		})

		DescribeTable("maps views", func(testConfig viewToRequestTestConfig) {
			view := map[string]interface{}{}
			Expect(yaml.Unmarshal([]byte(testConfig.view), &view)).To(Succeed())
			preconditionValidationResult := &preconditionValidationResult{
				k8sName:      "dash0-view",
				k8sNamespace: TestNamespaceName,
				resource:     view,
			}
			resourceToRequestsResult :=
				viewReconciler.MapResourceToHttpRequests(
					preconditionValidationResult,
					upsertAction,
					&logger,
				)
			Expect(resourceToRequestsResult.ItemsTotal).To(Equal(1))
			Expect(resourceToRequestsResult.OriginsInResource).To(BeNil())
			Expect(resourceToRequestsResult.ValidationIssues).To(BeNil())
			Expect(resourceToRequestsResult.SynchronizationErrors).To(BeNil())

			Expect(resourceToRequestsResult.HttpRequests).To(HaveLen(1))
			reqWithName := resourceToRequestsResult.HttpRequests[0]
			Expect(reqWithName.ItemName).To(Equal("dash0-view"))
			req := reqWithName.Request
			defer func() {
				_ = req.Body.Close()
			}()
			body, err := io.ReadAll(req.Body)
			Expect(err).ToNot(HaveOccurred())
			resultingViewInRequest := map[string]interface{}{}
			Expect(json.Unmarshal(body, &resultingViewInRequest)).To(Succeed())
			Expect(resultingViewInRequest["spec"]).ToNot(BeNil())

			Expect(resultingViewInRequest["metadata"]).ToNot(BeNil())
			Expect(ReadFromMap(resultingViewInRequest, []string{"metadata", "name"})).To(Equal("dash0-view"))

			if testConfig.expectedAnnotations != nil {
				annotationsRaw := ReadFromMap(resultingViewInRequest, []string{"metadata", "annotations"})
				Expect(annotationsRaw).ToNot(BeNil())
				annotations := annotationsRaw.(map[string]interface{})
				Expect(annotations).To(HaveLen(len(testConfig.expectedAnnotations)))
				for expectedKey, expectedValue := range testConfig.expectedAnnotations {
					value, ok := annotations[expectedKey]
					Expect(ok).To(BeTrue())
					Expect(value).To(Equal(expectedValue))
				}
			} else {
				Expect(ReadFromMap(resultingViewInRequest, []string{"metadata", "annotations"})).To(BeNil())
			}
		},
			Entry("should map view", viewToRequestTestConfig{
				view: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0View
metadata:
  name: dash0-view
spec:
  display:
    name: Dash0 View Example
    description: This is an example view.
  filter:
    - key: http.status_code
      operator: gte
      value:
        stringValue: "200"
`,
			}),
			Entry("should send annotations", viewToRequestTestConfig{
				view: `
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0View
metadata:
  name: dash0-view
  annotations:
    dash0com/annotation1: value1
    dash0com/annotation2: value2
spec:
  display:
    name: Dash0 View Example
    description: This is an example view.
  filter:
    - key: http.status_code
      operator: gte
      value:
        stringValue: "200"
`,
				expectedAnnotations: map[string]string{
					"dash0com/annotation1": "value1",
					"dash0com/annotation2": "value2",
				},
			}),
		)
	})
})

func createViewReconciler() *ViewReconciler {
	viewReconciler := NewViewReconciler(
		k8sClient,
		ClusterUidTest,
		viewLeaderElectionAware,
		&http.Client{},
	)
	return viewReconciler
}

func expectViewPutRequest(expectedPath string) {
	gock.New(ApiEndpointTest).
		Put(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(200).
		JSON(viewResponseWithIdOriginDataset)
}

func expectViewDeleteRequest(expectedPath string) {
	expectViewDeleteRequestWithHttpStatus(expectedPath, http.StatusOK)
}

func expectViewDeleteRequestWithHttpStatus(expectedPath string, status int) {
	gock.New(ApiEndpointTest).
		Delete(expectedPath).
		MatchHeader("Authorization", AuthorizationHeaderTest).
		MatchParam("dataset", DatasetCustomTest).
		Times(1).
		Reply(status).
		JSON(map[string]string{})
}

func createViewResource(namespace string, name string) *dash0v1alpha1.Dash0View {
	return createViewResourceWithEnableLabel(namespace, name, "")
}

func createViewResourceWithEnableLabel(namespace string, name string, dash0EnableLabelValue string) *dash0v1alpha1.Dash0View {
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
	if dash0EnableLabelValue != "" {
		objectMeta.Labels = map[string]string{
			"dash0.com/enable": dash0EnableLabelValue,
		}
	}
	return &dash0v1alpha1.Dash0View{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.dash0.com/v1alpha1",
			Kind:       "Dash0View",
		},
		ObjectMeta: objectMeta,
		Spec: dash0v1alpha1.Dash0ViewSpec{
			Type: "resources",
			Display: dash0v1alpha1.Dash0ViewDisplay{
				Name: "Test View",
			},
			Filter: []dash0v1alpha1.Dash0ViewFilter{
				{
					Key:      "service.name",
					Operator: "is",
					Value: &dash0v1alpha1.AnyValue{
						StringValue: &[]string{"test-service"}[0],
					},
				},
			},
			Table: &dash0v1alpha1.Dash0ViewTable{
				Columns: []dash0v1alpha1.Dash0ViewTableColumn{
					{
						Key: "service.name",
					},
				},
				Sort: []dash0v1alpha1.Dash0ViewTableSort{
					{
						Key:       "service.name",
						Direction: "ascending",
					},
				},
			},
			Visualizations: []dash0v1alpha1.Dash0ViewVisualization{
				{
					Renderers: []dash0v1alpha1.Dash0ViewRenderer{"resources/services/red"},
				},
			},
		},
	}
}

func deleteViewResourceIfItExists(ctx context.Context, k8sClient client.Client, namespace string, name string) {
	view := &dash0v1alpha1.Dash0View{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(ctx, view, &client.DeleteOptions{
		GracePeriodSeconds: new(int64),
	})
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyViewSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectIdOriginAndDataset bool,
	expectedError string,
) {
	Eventually(func(g Gomega) {
		view := &dash0v1alpha1.Dash0View{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, view)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(view.Status.SynchronizationStatus).To(Equal(expectedStatus))
		g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
		g.Expect(view.Status.SynchronizedAt.Time).To(BeTemporally("<=", time.Now()))
		if expectIdOriginAndDataset {
			g.Expect(view.Status.Dash0Id).To(Equal(viewId))
			g.Expect(view.Status.Dash0Origin).To(Equal(viewOrigin))
			g.Expect(view.Status.Dash0Dataset).To(Equal(DatasetCustomTest))
		}
		g.Expect(view.Status.SynchronizationError).To(ContainSubstring(expectedError))
		// views have no operator-side validations, all local validations are encoded in the CRD already
		g.Expect(view.Status.ValidationIssues).To(BeNil())
	}).Should(Succeed())
}

func verifyViewHasNoSynchronizationStatus(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	name string,
) {
	Eventually(func(g Gomega) {
		view := &dash0v1alpha1.Dash0View{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, view)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(view.Status.SynchronizationStatus)).To(Equal(""))
		g.Expect(view.Status.SynchronizationError).To(Equal(""))
		g.Expect(view.Status.ValidationIssues).To(BeNil())
	}).Should(Succeed())
}
