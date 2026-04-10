// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package allowlistreadycheck

import (
	"context"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("Waiting for the AllowlistSynchronizer to become ready (pre-install hook)", Ordered, func() {

	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	Context("when the AllowlistSynchronizer CRD does not exist", func() {
		It("should return immediately if the AllowlistSynchronizer CRD does not exist", func() {
			Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(Succeed())
		})
	})

	Context("when AllowlistSynchronizer CRD exists", Ordered, func() {

		BeforeAll(func() {
			installAllowlistSynchronizerCrd()
		})

		AfterAll(func() {
			uninstallAllowlistSynchronizerCrd(ctx)
		})

		AfterEach(func() {
			deleteAllowlistSynchronizerIfExists(ctx)
		})

		It("should time out if the AllowlistSynchronizer resource does not exist", func() {
			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(
					MatchError(ContainSubstring(
						"waiting for AllowlistSynchronizer dash0-allowlist-synchronizer to become ready has timed out",
					)),
				)
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				verifyTimeout(g, elapsedTimeNanoseconds)
			}, preInstallHandlerTimeoutForTests, 100*time.Millisecond).Should(Succeed())
		})

		It("should time out if the AllowlistSynchronizer resource exists but has no status conditions", func() {
			createAllowlistSynchronizer(ctx, nil, nil)

			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(
					MatchError(ContainSubstring(
						"waiting for AllowlistSynchronizer dash0-allowlist-synchronizer to become ready has timed out",
					)),
				)
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				verifyTimeout(g, elapsedTimeNanoseconds)
			}, preInstallHandlerTimeoutForTests, 100*time.Millisecond).Should(Succeed())
		})

		It("should time out if the AllowlistSynchronizer resource exists but Ready=False", func() {
			createAllowlistSynchronizer(ctx, []any{
				map[string]any{
					"type":               allowlistSynchronizerReadyConditionType,
					"status":             "False",
					"lastTransitionTime": "2026-01-01T00:00:00Z",
					"reason":             "SyncFailed",
					"message":            "synchronization failed",
				},
			}, nil)

			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(
					MatchError(ContainSubstring(
						"waiting for AllowlistSynchronizer dash0-allowlist-synchronizer to become ready has timed out",
					)),
				)
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				verifyTimeout(g, elapsedTimeNanoseconds)
			}, preInstallHandlerTimeoutForTests, 100*time.Millisecond).Should(Succeed())
		})

		It("should time out if Ready=True but managedAllowlistStatus is absent", func() {
			createAllowlistSynchronizer(ctx, []any{
				map[string]any{
					"type":               allowlistSynchronizerReadyConditionType,
					"status":             allowlistSynchronizerReadyConditionStatus,
					"lastTransitionTime": "2026-01-01T00:00:00Z",
					"reason":             "SyncSuccessful",
					"message":            "Synchronization completed successfully; allowlists up to date",
				},
			}, nil)

			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(
					MatchError(ContainSubstring(
						"waiting for AllowlistSynchronizer dash0-allowlist-synchronizer to become ready has timed out",
					)),
				)
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				verifyTimeout(g, elapsedTimeNanoseconds)
			}, preInstallHandlerTimeoutForTests, 100*time.Millisecond).Should(Succeed())
		})

		It("should time out if Ready=True but managedAllowlistStatus has wrong phase", func() {
			createAllowlistSynchronizer(ctx, []any{
				map[string]any{
					"type":               allowlistSynchronizerReadyConditionType,
					"status":             allowlistSynchronizerReadyConditionStatus,
					"lastTransitionTime": "2026-01-01T00:00:00Z",
					"reason":             "SyncSuccessful",
					"message":            "Synchronization completed successfully; allowlists up to date",
				},
			}, []any{
				map[string]any{
					"filePath":           "Dash0/operator/v1.0.0-test/dash0-operator-manager-v1.0.0-test.yaml",
					"generation":         int64(1),
					"lastSuccessfulSync": "2026-04-13T11:44:20Z",
					"phase":              "Pending",
				},
			})

			startTime := time.Now()
			var elapsedTimeNanoseconds int64

			go func() {
				defer GinkgoRecover()
				Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(
					MatchError(ContainSubstring(
						"waiting for AllowlistSynchronizer dash0-allowlist-synchronizer to become ready has timed out",
					)),
				)
				elapsedTimeNanoseconds = time.Since(startTime).Nanoseconds()
			}()

			Eventually(func(g Gomega) {
				verifyTimeout(g, elapsedTimeNanoseconds)
			}, preInstallHandlerTimeoutForTests, 100*time.Millisecond).Should(Succeed())
		})

		It("should succeed immediately if Ready=True and managedAllowlistStatus has the correct entry", func() {
			createAllowlistSynchronizer(ctx, []any{
				map[string]any{
					"type":               allowlistSynchronizerReadyConditionType,
					"status":             allowlistSynchronizerReadyConditionStatus,
					"lastTransitionTime": "2026-01-01T00:00:00Z",
					"reason":             "SyncSuccessful",
					"message":            "Synchronization completed successfully; allowlists up to date",
				},
			}, []any{
				map[string]any{
					"filePath":           "Dash0/operator/v1.0.0-test/dash0-operator-manager-v1.0.0-test.yaml",
					"generation":         int64(1),
					"lastSuccessfulSync": "2026-04-13T11:44:20Z",
					"phase":              allowlistInstalledPhase,
				},
			})

			Expect(preInstallHandler.WaitForAllowlistSynchronizerToBecomeReady()).To(Succeed())
		})
	})
})

func installAllowlistSynchronizerCrd() {
	allowlistSynchronizerCrd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: gkeAutopilotAllowlistSynchronizerCrdName,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gkeAutopilotAllowlistSynchronizerGroup,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: gkeAutopilotAllowlistSynchronizerPlural,
				Kind:   gkeAutopilotAllowlistSynchronizerKind,
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    gkeAutopilotAllowlistSynchronizerVersion,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:                   "object",
							XPreserveUnknownFields: ptr.To(true),
						},
					},
				},
			},
		},
	}
	_, err := envtest.InstallCRDs(testEnv.Config, envtest.CRDInstallOptions{
		CRDs: []*apiextensionsv1.CustomResourceDefinition{allowlistSynchronizerCrd},
	})
	Expect(err).ToNot(HaveOccurred())
}

func uninstallAllowlistSynchronizerCrd(ctx context.Context) {
	Expect(apiExtensionsClientset.ApiextensionsV1().CustomResourceDefinitions().Delete(
		ctx,
		gkeAutopilotAllowlistSynchronizerCrdName,
		metav1.DeleteOptions{},
	)).To(Succeed())
}

func createAllowlistSynchronizer(ctx context.Context, conditions []any, managedAllowlistStatus []any) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroup + "/" + gkeAutopilotAllowlistSynchronizerVersion,
			"kind":       gkeAutopilotAllowlistSynchronizerKind,
			"metadata": map[string]any{
				"name": dash0AllowlistSynchronizerName,
			},
			"spec": map[string]any{
				"allowlistPaths": []any{"Dash0/operator/*"},
			},
		},
	}
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())

	if len(conditions) > 0 || len(managedAllowlistStatus) > 0 {
		if len(conditions) > 0 {
			Expect(unstructured.SetNestedSlice(obj.Object, conditions, "status", "conditions")).To(Succeed())
		}
		if len(managedAllowlistStatus) > 0 {
			Expect(unstructured.SetNestedSlice(obj.Object, managedAllowlistStatus, "status", "managedAllowlistStatus")).To(Succeed())
		}
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	}
}

func deleteAllowlistSynchronizerIfExists(ctx context.Context) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gkeAutopilotAllowlistSynchronizerGroup,
		Kind:    gkeAutopilotAllowlistSynchronizerKind,
		Version: gkeAutopilotAllowlistSynchronizerVersion,
	})
	obj.SetName(dash0AllowlistSynchronizerName)
	_ = k8sClient.Delete(ctx, obj)
}

func verifyTimeout(g Gomega, elapsedTimeNanoseconds int64) {
	g.Expect(elapsedTimeNanoseconds).ToNot(BeZero())
	g.Expect(elapsedTimeNanoseconds).To(
		BeNumerically(">=", expectedPreInstallHandlerTimeoutNanosecondsMin),
	)
	g.Expect(elapsedTimeNanoseconds).To(
		BeNumerically("<=", expectedPreInstallHandlerTimeoutNanosecondsMax),
	)
}
