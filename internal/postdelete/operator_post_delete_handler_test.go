// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postdelete

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testFinalizer       = "operator.dash0.com/test-finalizer"
	shortTimeoutInTests = 1 * time.Second
)

var (
	gkeAutopilotAllowlistSynchronizerGroupAndVersion = fmt.Sprintf(
		"%s/%s",
		gkeAutopilotAllowlistSynchronizerGroup,
		gkeAutopilotAllowlistSynchronizerVersion,
	)

	// "workloadallowlists.auto.gke.io"
	gkeAutopilotWorkloadAllowlistCrdName = fmt.Sprintf(
		"%ss.%s",
		strings.ToLower(gkeAutopilotWorkloadAllowlistKind),
		gkeAutopilotAllowlistSynchronizerGroup,
	)

	managedBySynchronizerLabels = map[string]string{managedBySynchronizerLabelKey: managedBySynchronizerLabelValue}

	backgroundCtx    context.Context
	cancelBackground context.CancelFunc
	backgroundTasks  sync.WaitGroup
)

var _ = Describe("Uninstalling the Dash0 operator (post-delete hook)", Ordered, func() {
	ctx := context.Background()

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	Describe("removes the AllowlistSynchronizer", func() {

		var mockLogger, capturingLogSink = NewCapturingLogger()

		BeforeEach(func() {
			capturingLogSink.Reset()
		})

		Describe("CRD does not exist", func() {
			It("should do nothing if the AllowlistSynchronizer CRD does not exist", func() {
				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())
				capturingLogSink.HasNoLogMessages(Default)
			})
		})

		Describe("CRD exists", func() {
			BeforeAll(func() {
				installAllowlistSynchronizerCrd()
			})

			AfterAll(func() {
				uninstallAllowlistSynchronizerCrd(ctx)

			})

			It("should do nothing if the AllowlistSynchronizer CRD exists but the dash0-allowlist-synchronizer does not", func() {
				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())
				capturingLogSink.HasLogMessage(Default, "The AllowlistSynchronizer dash0-allowlist-synchronizer does not exist.")
				capturingLogSink.HasLogMessage(Default, "No WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer is left.")
			})

			It("should delete the Dash0 AllowlistSynchronizer if it exists", func() {
				createAllowlistSynchronizer(ctx, dash0AllowlistSynchronizerName, false)

				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())
				capturingLogSink.HasLogMessage(Default, "The AllowlistSynchronizer dash0-allowlist-synchronizer has been deleted.")
				capturingLogSink.HasNoLogMessage(Default, fmt.Sprintf(
					"The AllowlistSynchronizer dash0-allowlist-synchronizer has not been deleted within the timeout of %d seconds.",
					int(postDeleteHandlerTimeoutForTests/time.Second)))

				VerifyResourceDoesNotExist(
					ctx,
					k8sClient,
					"",
					dash0AllowlistSynchronizerName,
					&unstructured.Unstructured{
						Object: map[string]any{
							"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
							"kind":       gkeAutopilotAllowlistSynchronizerKind,
						},
					},
				)
			})

			It("should not delete unrelated AllowlistSynchronizers", func() {
				name := "unrelated-allowlist-synchronizer"
				createAllowlistSynchronizer(ctx, name, false)
				DeferCleanup(func() {
					Expect(k8sClient.Delete(ctx, createAllowlistSynchronizerReceiverWithName(name))).To(Succeed())
				})

				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

				VerifyResourceExists(
					ctx,
					k8sClient,
					"",
					name,
					createAllowlistSynchronizerReceiver(),
				)
			})

			Describe("WorkloadAllowlist CRD exists", func() {
				BeforeAll(func() {
					installWorkloadAllowlistCrd()
				})

				AfterAll(func() {
					uninstallCrd(ctx, gkeAutopilotWorkloadAllowlistCrdName)
				})

				BeforeEach(func() {
					backgroundCtx, cancelBackground = context.WithCancel(ctx)
				})

				AfterEach(func() {
					cancelBackground()
					backgroundTasks.Wait()
					postDeleteHandler.setTimeout(postDeleteHandlerTimeoutForTests)
					deleteAllWorkloadAllowlists(ctx)
					synchronizer := createAllowlistSynchronizerReceiverWithName(dash0AllowlistSynchronizerName)
					Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, synchronizer))).To(Succeed())
					Eventually(func(g Gomega) {
						err := k8sClient.Get(ctx, client.ObjectKeyFromObject(synchronizer), synchronizer)
						g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
					}).Should(Succeed())
				})

				It("should remove the allowlist paths and wait for GKE to uninstall the allowlists before deleting the "+
					"synchronizer", func() {
					createAllowlistSynchronizer(ctx, dash0AllowlistSynchronizerName, false)
					createWorkloadAllowlist(ctx, "dash0-allowlist-1", managedBySynchronizerLabels, false)
					createWorkloadAllowlist(ctx, "dash0-allowlist-2", managedBySynchronizerLabels, false)
					fakeGkeHasUninstalledAllowlists := startFakeGkeAllowlistSynchronizerController()

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					Eventually(fakeGkeHasUninstalledAllowlists).Should(BeClosed())
					capturingLogSink.HasLogMessage(Default,
						"The allowlist paths have been removed from the AllowlistSynchronizer "+
							"dash0-allowlist-synchronizer, waiting for GKE to uninstall the WorkloadAllowlists.")
					capturingLogSink.HasLogMessage(Default, "The AllowlistSynchronizer dash0-allowlist-synchronizer has been deleted.")
					// The allowlists have been removed by the fake GKE controller, the handler did not delete them.
					capturingLogSink.HasNoLogMessage(Default,
						"Successfully requested the deletion of the WorkloadAllowlist dash0-allowlist-1.")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-1")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-2")
					VerifyResourceDoesNotExist(
						ctx,
						k8sClient,
						"",
						dash0AllowlistSynchronizerName,
						createAllowlistSynchronizerReceiver(),
					)
				})

				It("should delete the synchronizer and the allowlists if removing the allowlist paths is rejected", func() {
					createAllowlistSynchronizer(ctx, dash0AllowlistSynchronizerName, true)
					createWorkloadAllowlist(ctx, "dash0-allowlist-1", managedBySynchronizerLabels, false)

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					capturingLogSink.HasLogMessage(Default,
						"Removing the allowlist paths from the AllowlistSynchronizer dash0-allowlist-synchronizer has "+
							"failed, deleting it right away.")
					capturingLogSink.HasLogMessage(Default, "The AllowlistSynchronizer dash0-allowlist-synchronizer has been deleted.")
					capturingLogSink.HasLogMessage(Default,
						"Successfully requested the deletion of the WorkloadAllowlist dash0-allowlist-1.")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-1")
				})

				It("should delete leftover allowlists installed by the Dash0 synchronizer and keep unrelated allowlists", func() {
					postDeleteHandler.setTimeout(shortTimeoutInTests)
					createAllowlistSynchronizer(ctx, dash0AllowlistSynchronizerName, false)
					createWorkloadAllowlist(ctx, "dash0-allowlist-1", managedBySynchronizerLabels, false)
					createWorkloadAllowlist(ctx, "dash0-allowlist-2", managedBySynchronizerLabels, false)
					createWorkloadAllowlist(ctx, "unlabelled-allowlist", nil, false)
					createWorkloadAllowlist(ctx, "other-synchronizer-allowlist", map[string]string{
						"auto.gke.io/managed-by-synchronizer-other-allowlist-synchronizer": "true",
					}, false)

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-1")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-2")
					VerifyResourceExists(ctx, k8sClient, "", "unlabelled-allowlist", createWorkloadAllowlistReceiver())
					VerifyResourceExists(ctx, k8sClient, "", "other-synchronizer-allowlist", createWorkloadAllowlistReceiver())
					capturingLogSink.HasLogMessage(Default, "No WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer is left.")
				})

				It("should delete allowlists that are recreated while waiting for the removal of the allowlists", func() {
					createWorkloadAllowlist(ctx, "dash0-allowlist-1", managedBySynchronizerLabels, true)
					recreateAllowlistWhenDeletionHasBeenRequested("dash0-allowlist-1", "dash0-allowlist-2")

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					capturingLogSink.HasLogMessage(Default,
						"Successfully requested the deletion of the WorkloadAllowlist dash0-allowlist-2.")
					capturingLogSink.HasLogMessage(Default, "No WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer is left.")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-1")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-2")
				})

				It("should delete allowlists that are recreated shortly after no allowlist has been left", func() {
					createAllowlistAfterDelay("dash0-allowlist-1", postDeleteHandlerQuietPeriodForTests/4)

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					capturingLogSink.HasLogMessage(Default,
						"Successfully requested the deletion of the WorkloadAllowlist dash0-allowlist-1.")
					verifyWorkloadAllowlistDoesNotExist(ctx, "dash0-allowlist-1")
				})

				It("should log a warning and terminate successfully if allowlists are left after the timeout", func() {
					postDeleteHandler.setTimeout(shortTimeoutInTests)
					createWorkloadAllowlist(ctx, "dash0-allowlist-1", managedBySynchronizerLabels, true)

					Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())

					capturingLogSink.HasLogMessage(Default,
						"The removal of the WorkloadAllowlists installed by the Dash0 AllowlistSynchronizer has not "+
							"completed within the timeout of 1 seconds, 1 WorkloadAllowlist(s) are left: dash0-allowlist-1.")
				})
			})
		})
	})
})

func installAllowlistSynchronizerCrd() {
	// Setting spec.rejectEmptyAllowlistPaths makes the API server reject an update that sets spec.allowlistPaths to an
	// empty list.
	installCrd(
		gkeAutopilotAllowlistSynchronizerCrdName,
		gkeAutopilotAllowlistSynchronizerPlural,
		gkeAutopilotAllowlistSynchronizerKind,
		&apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"allowlistPaths": {
							Type:  "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}},
						},
						"rejectEmptyAllowlistPaths": {Type: "boolean"},
					},
					XValidations: apiextensionsv1.ValidationRules{
						{
							Rule: "!has(self.rejectEmptyAllowlistPaths) || !self.rejectEmptyAllowlistPaths || " +
								"(has(self.allowlistPaths) && size(self.allowlistPaths) > 0)",
						},
					},
				},
			},
		},
	)
}

func installWorkloadAllowlistCrd() {
	installCrd(
		gkeAutopilotWorkloadAllowlistCrdName,
		strings.ToLower(gkeAutopilotWorkloadAllowlistKind)+"s",
		gkeAutopilotWorkloadAllowlistKind,
		&apiextensionsv1.JSONSchemaProps{
			Type:                   "object",
			XPreserveUnknownFields: ptr.To(true),
		},
	)
}

func installCrd(name string, plural string, kind string, schema *apiextensionsv1.JSONSchemaProps) {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gkeAutopilotAllowlistSynchronizerGroup,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   kind,
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    gkeAutopilotAllowlistSynchronizerVersion,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: schema,
					},
				},
			},
		},
	}
	_, err := envtest.InstallCRDs(testEnv.Config, envtest.CRDInstallOptions{
		CRDs: []*apiextensionsv1.CustomResourceDefinition{crd},
	})
	Expect(err).ToNot(HaveOccurred())
}

func uninstallAllowlistSynchronizerCrd(ctx context.Context) {
	uninstallCrd(ctx, gkeAutopilotAllowlistSynchronizerCrdName)
}

func uninstallCrd(ctx context.Context, name string) {
	Expect(apiExtensionsClientset.ApiextensionsV1().CustomResourceDefinitions().Delete(
		ctx,
		name,
		metav1.DeleteOptions{},
	)).To(Succeed())
}

func createAllowlistSynchronizer(ctx context.Context, name string, rejectEmptyAllowlistPaths bool) {
	dash0AllowlistSynchronizer := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
			"kind":       gkeAutopilotAllowlistSynchronizerKind,
			"metadata": map[string]any{
				"name": name,
			},
			"spec": map[string]any{
				"allowlistPaths":            []any{"Dash0/operator/*"},
				"rejectEmptyAllowlistPaths": rejectEmptyAllowlistPaths,
			},
		},
	}
	Expect(k8sClient.Create(ctx, dash0AllowlistSynchronizer)).To(Succeed())
}

func createAllowlistSynchronizerReceiver() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
			"kind":       gkeAutopilotAllowlistSynchronizerKind,
		},
	}
}

func createAllowlistSynchronizerReceiverWithName(name string) *unstructured.Unstructured {
	synchronizer := createAllowlistSynchronizerReceiver()
	synchronizer.SetName(name)
	return synchronizer
}

func createWorkloadAllowlist(ctx context.Context, name string, labels map[string]string, withFinalizer bool) {
	allowlist := createWorkloadAllowlistReceiver()
	allowlist.SetName(name)
	allowlist.SetLabels(labels)
	if withFinalizer {
		allowlist.SetFinalizers([]string{testFinalizer})
	}
	Expect(k8sClient.Create(ctx, allowlist)).To(Succeed())
}

func createWorkloadAllowlistReceiver() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
			"kind":       gkeAutopilotWorkloadAllowlistKind,
		},
	}
}

func removeFinalizers(ctx context.Context, allowlist *unstructured.Unstructured) {
	Expect(client.IgnoreNotFound(k8sClient.Patch(
		ctx,
		allowlist,
		client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`)),
	))).To(Succeed())
}

func deleteAllWorkloadAllowlists(ctx context.Context) {
	allowlists := &unstructured.UnstructuredList{}
	allowlists.SetGroupVersionKind(workloadAllowlistListGvk)
	Expect(k8sClient.List(ctx, allowlists)).To(Succeed())
	for i := range allowlists.Items {
		removeFinalizers(ctx, &allowlists.Items[i])
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &allowlists.Items[i]))).To(Succeed())
	}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.List(ctx, allowlists)).To(Succeed())
		g.Expect(allowlists.Items).To(BeEmpty())
	}).Should(Succeed())
}

func verifyWorkloadAllowlistDoesNotExist(ctx context.Context, name string) {
	VerifyResourceDoesNotExist(ctx, k8sClient, "", name, createWorkloadAllowlistReceiver())
}

// runInBackground runs task in a goroutine until it returns or the background context is cancelled, which happens
// in AfterEach before the test resources are cleaned up.
func runInBackground(task func(ctx context.Context)) {
	backgroundTasks.Add(1)
	go func() {
		defer backgroundTasks.Done()
		defer GinkgoRecover()
		task(backgroundCtx)
	}()
}

// sleepOrCancel returns false if ctx has been cancelled before the duration has passed.
func sleepOrCancel(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

// startFakeGkeAllowlistSynchronizerController mimics GKE: once the allowlist paths of the Dash0 AllowlistSynchronizer
// are empty, it uninstalls the allowlists after a delay. The returned channel is closed after the uninstall.
func startFakeGkeAllowlistSynchronizerController() <-chan struct{} {
	uninstalled := make(chan struct{})
	runInBackground(func(ctx context.Context) {
		for sleepOrCancel(ctx, 50*time.Millisecond) {
			synchronizer := createAllowlistSynchronizerReceiver()
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: dash0AllowlistSynchronizerName}, synchronizer); err != nil {
				continue
			}
			paths, found, _ := unstructured.NestedSlice(synchronizer.Object, "spec", "allowlistPaths")
			if !found || len(paths) > 0 {
				continue
			}
			if !sleepOrCancel(ctx, time.Second) {
				return
			}
			Expect(k8sClient.DeleteAllOf(
				ctx,
				createWorkloadAllowlistReceiver(),
				client.MatchingLabels(managedBySynchronizerLabels),
			)).To(Succeed())
			close(uninstalled)
			return
		}
	})
	return uninstalled
}

// recreateAllowlistWhenDeletionHasBeenRequested creates a new allowlist as soon as the deletion of the existing
// allowlist (which has a finalizer) has been requested, and then lets the deletion of the existing allowlist complete.
func recreateAllowlistWhenDeletionHasBeenRequested(existingName string, recreatedName string) {
	runInBackground(func(ctx context.Context) {
		existing := createWorkloadAllowlistReceiver()
		for sleepOrCancel(ctx, 50*time.Millisecond) {
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: existingName}, existing); err != nil ||
				existing.GetDeletionTimestamp() == nil {
				continue
			}
			createWorkloadAllowlist(ctx, recreatedName, managedBySynchronizerLabels, false)
			removeFinalizers(ctx, existing)
			return
		}
	})
}

func createAllowlistAfterDelay(name string, delay time.Duration) {
	runInBackground(func(ctx context.Context) {
		if sleepOrCancel(ctx, delay) {
			createWorkloadAllowlist(ctx, name, managedBySynchronizerLabels, false)
		}
	})
}
