// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0dashv1alpha1 "github.com/dash0hq/dash0-operator/api/dash0/v1alpha1"
	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	"github.com/h2non/gock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	datasetName          = "staging-dataset"
	datasetSlug          = "staging"
	datasetDisplayName   = "Staging"
	datasetCollection    = "/api/datasets"
	datasetSlugPath      = "/api/datasets/staging"
	datasetOriginPattern = "dash0-operator_%s_test-namespace_staging-dataset"
)

var datasetLeaderElectionAware = NewLeaderElectionAwareMock(true)

var _ = Describe(
	"The Dataset controller", Ordered, func() {
		var (
			testStartedAt     time.Time
			clusterId         string
			datasetReconciler *DatasetReconciler
		)

		ctx := context.Background()
		logger := logd.FromContext(ctx)

		reconcileDatasetAndReturnError := func() error {
			GinkgoHelper()
			result, err := datasetReconciler.Reconcile(
				ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName},
				},
			)
			Expect(result).To(Equal(reconcile.Result{}))
			return err
		}

		reconcileDataset := func() {
			GinkgoHelper()
			Expect(reconcileDatasetAndReturnError()).To(Succeed())
		}

		deleteDatasetDeleteEndpoint := func() *gock.Request {
			return gock.New(ApiEndpointTest).
				Delete(fmt.Sprintf("%s/%s", datasetCollection, fmt.Sprintf(datasetOriginPattern, clusterId))).
				MatchHeader("Authorization", AuthorizationHeaderTest)
		}

		createAndDeleteDatasetResourceWithFinalizer := func() {
			GinkgoHelper()
			dataset := createDatasetResource()
			dataset.Finalizers = []string{dash0dashv1alpha1.DatasetFinalizerId}
			Expect(k8sClient.Create(ctx, dataset)).To(Succeed())
			Expect(k8sClient.Delete(ctx, dataset)).To(Succeed())
		}

		BeforeAll(
			func() {
				EnsureTestNamespaceExists(ctx, k8sClient)
				EnsureOperatorNamespaceExists(ctx, k8sClient)
				clusterId = string(cluster.ReadPseudoClusterUid(ctx, k8sClient, logger))
			},
		)

		BeforeEach(
			func() {
				testStartedAt = time.Now()
				datasetReconciler = NewDatasetReconciler(
					k8sClient,
					types.UID(clusterId),
					datasetLeaderElectionAware,
					NewApiClientPool(
						dash0apiclient.NewTransport(
							dash0apiclient.WithBaseTransport(gockTransportProxy{}),
							dash0apiclient.WithTransportMaxRetries(2),
							dash0apiclient.WithTransportRetryWaitMin(1*time.Millisecond),
							dash0apiclient.WithTransportRetryWaitMax(5*time.Millisecond),
							dash0apiclient.WithTransportRetryOnConflict(),
						),
						"dash0-operator-test",
					),
				)
				datasetReconciler.defaultApiConfigs.Set(
					[]ApiConfig{{Endpoint: ApiEndpointTest, Dataset: DatasetCustomTest, Token: AuthorizationTokenTest}},
				)
				EnsureMonitoringResourceWithoutExportExistsAndIsAvailable(ctx, k8sClient)
			},
		)

		AfterEach(
			func() {
				gock.Off()
				VerifyNoUnmatchedGockRequests()
				deleteDatasetResourceIfItExists(ctx)
				DeleteMonitoringResourceIfItExists(ctx, k8sClient)
			},
		)

		It(
			"creates the dataset with POST and sets its permissions with PUT when it does not exist", func() {
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(1).Reply(404)
				gock.New(ApiEndpointTest).
					Post(datasetCollection).
					MatchHeader("Authorization", AuthorizationHeaderTest).
					AddMatcher(datasetRequestBodyMatcher(fmt.Sprintf(datasetOriginPattern, clusterId))).
					Times(1).
					Reply(201).
					JSON(datasetResponse(clusterId))
				gock.New(ApiEndpointTest).
					Put(datasetSlugPath).
					MatchHeader("Authorization", AuthorizationHeaderTest).
					AddMatcher(datasetPermissionsMatcher("basic_member")).
					Times(1).
					Reply(200).
					JSON(datasetResponse(clusterId))

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
					testStartedAt,
					datasetSlug,
					fmt.Sprintf(datasetOriginPattern, clusterId),
					"",
				)
				Expect(getDatasetResource(ctx).Finalizers).To(ContainElement(dash0dashv1alpha1.DatasetFinalizerId))
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"updates the dataset with PUT when it exists", func() {
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(1).Reply(200).JSON(datasetResponse(clusterId))
				gock.New(ApiEndpointTest).
					Put(datasetSlugPath).
					AddMatcher(datasetRequestBodyMatcher(fmt.Sprintf(datasetOriginPattern, clusterId))).
					Times(1).
					Reply(200).
					JSON(datasetResponse(clusterId))

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
					testStartedAt,
					datasetSlug,
					fmt.Sprintf(datasetOriginPattern, clusterId),
					"",
				)
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"retries an update that fails with a 409 version conflict", func() {
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(1).Reply(200).JSON(datasetResponse(clusterId))
				gock.New(ApiEndpointTest).Put(datasetSlugPath).Times(1).Reply(409).
					JSON(map[string]any{"message": "dataset version conflict, retry"})
				gock.New(ApiEndpointTest).Put(datasetSlugPath).Times(1).Reply(200).JSON(datasetResponse(clusterId))

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
					testStartedAt,
					datasetSlug,
					fmt.Sprintf(datasetOriginPattern, clusterId),
					"",
				)
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"neither creates nor updates the dataset when the lookup fails with an error other than 404", func() {
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(3).Reply(500)

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusFailed,
					testStartedAt,
					"",
					"",
					"unexpected status code 500",
				)
				dataset := getDatasetResource(ctx)
				Expect(dataset.Status.SynchronizationResults[0].HttpStatusCode).To(Equal(http.StatusInternalServerError))
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"adopts an existing dataset that has no origin", func() {
				existing := datasetResponse(clusterId)
				delete(existing["metadata"].(map[string]any)["labels"].(map[string]any), "dash0.com/origin")
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(1).Reply(200).JSON(existing)
				gock.New(ApiEndpointTest).
					Put(datasetSlugPath).
					AddMatcher(datasetRequestBodyMatcher(fmt.Sprintf(datasetOriginPattern, clusterId))).
					Times(1).
					Reply(200).
					JSON(datasetResponse(clusterId))

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusSuccessful,
					testStartedAt,
					datasetSlug,
					fmt.Sprintf(datasetOriginPattern, clusterId),
					"",
				)
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"does not update an existing dataset that has another origin", func() {
				existing := datasetResponse(clusterId)
				existing["metadata"].(map[string]any)["labels"].(map[string]any)["dash0.com/origin"] = "terraform"
				gock.New(ApiEndpointTest).Get(datasetSlugPath).Times(1).Reply(200).JSON(existing)

				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				verifyDatasetSynchronizationStatus(
					ctx,
					dash0common.Dash0ApiResourceSynchronizationStatusFailed,
					testStartedAt,
					"",
					"",
					"is managed by another origin (terraform)",
				)
				dataset := getDatasetResource(ctx)
				Expect(dataset.Status.SynchronizationResults[0].HttpStatusCode).To(Equal(http.StatusConflict))
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"deletes the dataset by its origin and removes the finalizer when the resource is deleted", func() {
				deleteDatasetDeleteEndpoint().Times(1).Reply(200)

				createAndDeleteDatasetResourceWithFinalizer()
				reconcileDataset()

				verifyDatasetResourceIsGone(ctx)
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"removes the finalizer when the dataset does not exist in Dash0", func() {
				deleteDatasetDeleteEndpoint().Times(1).Reply(404)

				createAndDeleteDatasetResourceWithFinalizer()
				reconcileDataset()

				verifyDatasetResourceIsGone(ctx)
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"keeps the finalizer and returns an error when the delete fails", func() {
				deleteDatasetDeleteEndpoint().Times(3).Reply(500)

				createAndDeleteDatasetResourceWithFinalizer()
				Expect(reconcileDatasetAndReturnError()).To(MatchError(ContainSubstring("unexpected status code 500")))

				Expect(getDatasetResource(ctx).Finalizers).To(ContainElement(dash0dashv1alpha1.DatasetFinalizerId))
				Expect(gock.IsDone()).To(BeTrue())
			},
		)

		It(
			"removes the finalizer without deleting the dataset when there is no monitoring resource", func() {
				createAndDeleteDatasetResourceWithFinalizer()
				DeleteMonitoringResourceIfItExists(ctx, k8sClient)
				reconcileDataset()

				verifyDatasetResourceIsGone(ctx)
			},
		)

		It(
			"does not add the finalizer when the dataset is not synchronized", func() {
				DeleteMonitoringResourceIfItExists(ctx, k8sClient)
				Expect(k8sClient.Create(ctx, createDatasetResource())).To(Succeed())
				reconcileDataset()

				Expect(getDatasetResource(ctx).Finalizers).NotTo(ContainElement(dash0dashv1alpha1.DatasetFinalizerId))
			},
		)

		It(
			"keeps the finalizer and retries when the API config of the namespace has not been loaded yet", func() {
				DeleteMonitoringResourceIfItExists(ctx, k8sClient)
				EnsureMonitoringResourceExistsAndIsAvailable(ctx, k8sClient)
				createAndDeleteDatasetResourceWithFinalizer()

				result, err := datasetReconciler.Reconcile(
					ctx, reconcile.Request{
						NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName},
					},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(datasetDeleteRetryDelay))
				Expect(getDatasetResource(ctx).Finalizers).To(ContainElement(dash0dashv1alpha1.DatasetFinalizerId))
			},
		)

		It(
			"keeps the finalizer and retries when the operator has just started and has no API config yet", func() {
				datasetReconciler.defaultApiConfigs.Clear()
				createAndDeleteDatasetResourceWithFinalizer()

				result, err := datasetReconciler.Reconcile(
					ctx, reconcile.Request{
						NamespacedName: types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName},
					},
				)

				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(datasetDeleteRetryDelay))
				Expect(getDatasetResource(ctx).Finalizers).To(ContainElement(dash0dashv1alpha1.DatasetFinalizerId))
			},
		)

		It(
			"removes the finalizer when there is still no API config after the startup grace period", func() {
				datasetReconciler.defaultApiConfigs.Clear()
				datasetReconciler.startedAt = time.Now().Add(-datasetApiConfigStartupGracePeriod)
				createAndDeleteDatasetResourceWithFinalizer()
				reconcileDataset()

				verifyDatasetResourceIsGone(ctx)
			},
		)

		It(
			"maps the resource to a dataset definition", func() {
				resource, err := structToMap(createDatasetResource())
				Expect(err).NotTo(HaveOccurred())

				datasetDefinition, err := mapToDatasetDefinition(resource.Object, "test-origin")
				Expect(err).NotTo(HaveOccurred())

				body, err := json.Marshal(datasetDefinition)
				Expect(err).NotTo(HaveOccurred())
				Expect(body).To(MatchJSON(`{
					"apiVersion": "dash0.com/v1alpha1",
					"kind": "Dash0Dataset",
					"metadata": {
						"name": "Staging",
						"labels": {"dash0.com/id": "staging", "dash0.com/origin": "test-origin"}
					},
					"spec": {
						"slug": "staging",
						"preferred": true,
						"permissions": [{"actions": ["dataset:read"], "role": "basic_member"}],
						"privacy": {"redaction": {"cardholderData": {"enabled": true}}},
						"retention": {"logs": "90d"}
					}
				}`))
			},
		)
	},
)

type gockTransportProxy struct{}

func (gockTransportProxy) RoundTrip(req *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(req)
}

func datasetRequestBodyMatcher(expectedOrigin string) gock.MatchFunc {
	return func(req *http.Request, _ *gock.Request) (bool, error) {
		definition := dash0apiclient.DatasetDefinition{}
		if err := json.NewDecoder(req.Body).Decode(&definition); err != nil {
			return false, err
		}
		labels := definition.Metadata.Labels
		return definition.Spec.Slug == datasetSlug &&
			definition.Metadata.Name == datasetDisplayName &&
			labels != nil && labels.Dash0Comorigin != nil && *labels.Dash0Comorigin == expectedOrigin, nil
	}
}

func datasetPermissionsMatcher(expectedRole string) gock.MatchFunc {
	return func(req *http.Request, _ *gock.Request) (bool, error) {
		definition := dash0apiclient.DatasetDefinition{}
		if err := json.NewDecoder(req.Body).Decode(&definition); err != nil {
			return false, err
		}
		permissions := definition.Spec.Permissions
		return permissions != nil && len(*permissions) == 1 &&
			(*permissions)[0].Role != nil && *(*permissions)[0].Role == expectedRole, nil
	}
}

func datasetResponse(clusterId string) map[string]any {
	return map[string]any{
		"apiVersion": "dash0.com/v1alpha1",
		"kind":       "Dash0Dataset",
		"metadata": map[string]any{
			"name": datasetDisplayName,
			"labels": map[string]any{
				"dash0.com/id":     datasetSlug,
				"dash0.com/origin": fmt.Sprintf(datasetOriginPattern, clusterId),
				"dash0.com/source": "operator",
			},
		},
		"spec": map[string]any{"slug": datasetSlug},
	}
}

func createDatasetResource() *dash0dashv1alpha1.Dash0Dataset {
	return &dash0dashv1alpha1.Dash0Dataset{
		TypeMeta:   metav1.TypeMeta{APIVersion: "dash0.com/v1alpha1", Kind: "Dash0Dataset"},
		ObjectMeta: metav1.ObjectMeta{Name: datasetName, Namespace: TestNamespaceName},
		Spec: dash0dashv1alpha1.Dash0DatasetSpec{
			Slug:      datasetSlug,
			Display:   dash0dashv1alpha1.Dash0DatasetDisplay{Name: datasetDisplayName},
			Preferred: new(true),
			Permissions: []dash0dashv1alpha1.Dash0DatasetPermission{
				{Actions: []dash0dashv1alpha1.Dash0DatasetAction{"dataset:read"}, Role: new("basic_member")},
			},
			Privacy: &dash0dashv1alpha1.Dash0DatasetPrivacy{
				Redaction: &dash0dashv1alpha1.Dash0DatasetRedaction{
					CardholderData: &dash0dashv1alpha1.Dash0DatasetCardholderDataRedaction{Enabled: true},
				},
			},
			Retention: &dash0dashv1alpha1.Dash0DatasetRetention{Logs: new("90d")},
		},
	}
}

func getDatasetResource(ctx context.Context) *dash0dashv1alpha1.Dash0Dataset {
	GinkgoHelper()
	dataset := &dash0dashv1alpha1.Dash0Dataset{}
	Expect(k8sClient.Get(
		ctx, types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName}, dataset,
	)).To(Succeed())
	return dataset
}

func verifyDatasetResourceIsGone(ctx context.Context) {
	GinkgoHelper()
	err := k8sClient.Get(
		ctx, types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName}, &dash0dashv1alpha1.Dash0Dataset{},
	)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected the dataset resource to be gone, got %v", err)
}

func deleteDatasetResourceIfItExists(ctx context.Context) {
	dataset := &dash0dashv1alpha1.Dash0Dataset{}
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName}, dataset)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	if len(dataset.Finalizers) > 0 {
		dataset.Finalizers = nil
		Expect(k8sClient.Update(ctx, dataset)).To(Succeed())
	}
	err = k8sClient.Delete(
		ctx,
		&dash0dashv1alpha1.Dash0Dataset{ObjectMeta: metav1.ObjectMeta{Name: datasetName, Namespace: TestNamespaceName}},
		&client.DeleteOptions{GracePeriodSeconds: new(int64)},
	)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func verifyDatasetSynchronizationStatus(
	ctx context.Context,
	expectedStatus dash0common.Dash0ApiResourceSynchronizationStatus,
	testStartedAt time.Time,
	expectedId string,
	expectedOrigin string,
	expectedError string,
) {
	GinkgoHelper()
	Eventually(
		func(g Gomega) {
			dataset := &dash0dashv1alpha1.Dash0Dataset{}
			g.Expect(k8sClient.Get(
				ctx, types.NamespacedName{Namespace: TestNamespaceName, Name: datasetName}, dataset,
			)).To(Succeed())
			g.Expect(dataset.Status.SynchronizationStatus).To(Equal(expectedStatus))
			g.Expect(dataset.Status.SynchronizedAt.Time).To(BeTemporally(">=", testStartedAt.Add(-1*time.Second)))
			g.Expect(dataset.Status.SynchronizationResults).To(HaveLen(1))
			result := dataset.Status.SynchronizationResults[0]
			g.Expect(result.Dash0ApiEndpoint).To(Equal(ApiEndpointStandardizedTest))
			g.Expect(result.Dash0Id).To(Equal(expectedId))
			g.Expect(result.Dash0Origin).To(Equal(expectedOrigin))
			g.Expect(result.SynchronizationError).To(ContainSubstring(expectedError))
		},
	).Should(Succeed())
}
