// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The collector reconciler", func() {
	Describe("the target-allocator mTLS client cert secret predicate", func() {
		var collectorManager *CollectorManager
		var reconciler *CollectorReconciler

		BeforeEach(func() {
			collectorManager = NewCollectorManager(
				k8sClient,
				nodeMetadataClient,
				util.ExtraConfigDefaults,
				util.ExtraConfigDefaults,
				false,
				false,
				nil,
			)
			reconciler = NewCollectorReconciler(k8sClient, collectorManager, OperatorNamespace, OTelCollectorNamePrefixTest)
		})

		secret := func(namespace string, name string) *corev1.Secret {
			return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
		}

		secretWithCerts := func(caCert string, clientCert string) *corev1.Secret {
			s := secret(OperatorNamespace, "client-cert")
			s.Data = map[string][]byte{"ca.crt": []byte(caCert), "tls.crt": []byte(clientCert)}
			return s
		}

		enableMtls := func(clientCertSecretName string) {
			extraConfig := util.ExtraConfigDefaults
			extraConfig.TargetAllocatorMtlsEnabled = true
			extraConfig.TargetAllocatorMtlsServerCertSecretName = "server-cert"
			extraConfig.TargetAllocatorMtlsClientCertSecretName = clientCertSecretName
			collectorManager.storeTargetAllocatorMtlsClientCertSecretName(extraConfig)
		}

		It("should not match any secret before the first reconciliation", func() {
			p := reconciler.targetAllocatorMtlsClientCertSecretPredicate()
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "")})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "client-cert")})).To(BeFalse())
		})

		It("should not match any secret if mTLS is disabled", func() {
			collectorManager.storeTargetAllocatorMtlsClientCertSecretName(util.ExtraConfigDefaults)
			p := reconciler.targetAllocatorMtlsClientCertSecretPredicate()
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "")})).To(BeFalse())
		})

		It("should only match the client cert secret in the operator namespace", func() {
			enableMtls("client-cert")
			p := reconciler.targetAllocatorMtlsClientCertSecretPredicate()

			clientCertSecret := secret(OperatorNamespace, "client-cert")
			Expect(p.Create(event.CreateEvent{Object: clientCertSecret})).To(BeTrue())
			Expect(p.Delete(event.DeleteEvent{Object: clientCertSecret})).To(BeTrue())

			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "server-cert")})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: secret("other-namespace", "client-cert")})).To(BeFalse())
		})

		It("should only match updates of the client cert secret that change the CA certificate", func() {
			enableMtls("client-cert")
			p := reconciler.targetAllocatorMtlsClientCertSecretPredicate()

			Expect(p.Update(event.UpdateEvent{
				ObjectOld: secretWithCerts("ca-1", "client-cert-1"),
				ObjectNew: secretWithCerts("ca-2", "client-cert-2"),
			})).To(BeTrue())
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: secretWithCerts("", "client-cert-1"),
				ObjectNew: secretWithCerts("ca-1", "client-cert-1"),
			})).To(BeTrue())
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: secretWithCerts("ca-1", "client-cert-1"),
				ObjectNew: secretWithCerts("ca-1", "client-cert-2"),
			})).To(BeFalse())
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: secretWithCerts("ca-1", "client-cert-1"),
				ObjectNew: secretWithCerts("ca-1", "client-cert-1"),
			})).To(BeFalse())
		})

		It("should follow a change of the client cert secret name", func() {
			enableMtls("client-cert")
			p := reconciler.targetAllocatorMtlsClientCertSecretPredicate()
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "client-cert")})).To(BeTrue())

			enableMtls("renamed-client-cert")
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "client-cert")})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: secret(OperatorNamespace, "renamed-client-cert")})).To(BeTrue())
		})
	})
})
