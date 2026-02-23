// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postdelete

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var (
	gkeAutopilotAllowlistSynchronizerGroupAndVersion = fmt.Sprintf(
		"%s/%s",
		gkeAutopilotAllowlistSynchronizerGroup,
		gkeAutopilotAllowlistSynchronizerVersion,
	)
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
				capturingLogSink.HasNoLogMessages(Default)
			})

			It("should delete the Dash0 AllowlistSynchronizer if it exists", func() {
				createAllowlistSynchronizer(ctx, dash0AllowlistSynchronizerName)

				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())
				capturingLogSink.HasNoLogMessages(Default)

				VerifyResourceDoesNotExist(
					ctx,
					k8sClient,
					"",
					dash0AllowlistSynchronizerName,
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
							"kind":       gkeAutopilotAllowlistSynchronizerKind,
						},
					},
				)
			})

			It("should not delete unrelated AllowlistSynchronizers", func() {
				name := "unrelated-allowlist-synchronizer"
				createAllowlistSynchronizer(ctx, name)

				Expect(postDeleteHandler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, mockLogger)).To(Succeed())
				capturingLogSink.HasNoLogMessages(Default)

				VerifyResourceExists(
					ctx,
					k8sClient,
					"",
					name,
					createAllowlistSynchronizerReceiver(),
				)
			})
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
							Type: "object",
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

func createAllowlistSynchronizer(ctx context.Context, name string) {
	dash0AllowlistSynchronizer := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
			"kind":       gkeAutopilotAllowlistSynchronizerKind,
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{},
		},
	}
	Expect(k8sClient.Create(ctx, dash0AllowlistSynchronizer)).To(Succeed())
}

func createAllowlistSynchronizerReceiver() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gkeAutopilotAllowlistSynchronizerGroupAndVersion,
			"kind":       gkeAutopilotAllowlistSynchronizerKind,
		},
	}
}
