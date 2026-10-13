// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	testNamespace          = "dash0-system"
	testSecretName         = "dash0-operator-webhook-server-cert"
	testPreviousSecretName = "dash0-operator-certificates"
	testDeploymentName     = "dash0-operator-controller"
	testServiceName        = "dash0-operator-webhook-service"
	testMwcName            = "dash0-operator-injector"
	testVwcName            = "dash0-operator-monitoring-validator"
	testCrdName            = "dash0monitorings.operator.dash0.com"
	testPersesCrdName      = "persesdashboards.perses.dev"
	ownWebhookName         = "webhook.dash0.com"
	foreignWebhookName     = "webhook.example.com"
	foreignServiceName     = "some-other-webhook-service"
)

var foreignCaBundle = []byte("foreign-ca-bundle")

var _ = Describe("The webhook certificates manager", func() {
	ctx := context.Background()
	logger := logd.Discard()

	var (
		objects    []client.Object
		funcs      interceptor.Funcs
		config     Config
		baseClient client.WithWatch
		manager    *Manager
		now        time.Time
		recorder   *events.FakeRecorder
	)

	BeforeEach(func() {
		now = testNow
		objects = []client.Object{
			testSecret("secret-uid-1", nil),
			testMutatingWebhookConfiguration(testNamespace, nil),
			testValidatingWebhookConfiguration(testServiceName, nil),
			testCrd(testCrdName, testNamespace, nil),
			testDeployment(true),
		}
		funcs = interceptor.Funcs{}
		config = testConfig()
	})

	buildManager := func() {
		baseClient = fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objects...).Build()
		manager = newTestManager(config, interceptor.NewClient(baseClient, funcs), &now)
		recorder = events.NewFakeRecorder(100)
		manager.SetEventRecorder(recorder, testDeployment(true))
	}

	getSecret := func() *corev1.Secret {
		secret := &corev1.Secret{}
		Expect(baseClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testSecretName}, secret)).To(Succeed())
		return secret
	}

	storedMaterial := func() *material {
		stored, err := materialFromSecretData(getSecret().Data, now)
		Expect(err).ToNot(HaveOccurred())
		return stored
	}

	getMwc := func() *admissionregistrationv1.MutatingWebhookConfiguration {
		mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
		Expect(baseClient.Get(ctx, client.ObjectKey{Name: testMwcName}, mwc)).To(Succeed())
		return mwc
	}

	getVwc := func() *admissionregistrationv1.ValidatingWebhookConfiguration {
		vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		Expect(baseClient.Get(ctx, client.ObjectKey{Name: testVwcName}, vwc)).To(Succeed())
		return vwc
	}

	getCrd := func(name string) *apiextensionsv1.CustomResourceDefinition {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(baseClient.Get(ctx, client.ObjectKey{Name: name}, crd)).To(Succeed())
		return crd
	}

	mwcBundle := func(webhookName string) []byte {
		for _, webhook := range getMwc().Webhooks {
			if webhook.Name == webhookName {
				return webhook.ClientConfig.CABundle
			}
		}
		Fail(fmt.Sprintf("webhook %s not found", webhookName))
		return nil
	}

	vwcBundle := func() []byte {
		return getVwc().Webhooks[0].ClientConfig.CABundle
	}

	crdBundle := func(name string) []byte {
		return getCrd(name).Spec.Conversion.Webhook.ClientConfig.CABundle
	}

	resourceVersions := func() []string {
		return []string{
			getSecret().ResourceVersion,
			getMwc().ResourceVersion,
			getVwc().ResourceVersion,
			getCrd(testCrdName).ResourceVersion,
		}
	}

	receivedEvents := func() []string {
		var received []string
		for {
			select {
			case event := <-recorder.Events:
				received = append(received, event)
			default:
				return received
			}
		}
	}

	Describe("generating, adopting and restoring the material", func() {
		It("generates the material, loads it, injects the bundle and sets the readiness latch", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())

			stored := storedMaterial()
			bundle := stored.bundle()
			Expect(getSecret().Data[keyCaBundle]).To(Equal(bundle))
			Expect(manager.Store().CaBundle()).To(Equal(bundle))
			Expect(manager.Store().IsReady()).To(BeTrue())
			certificate, err := manager.Store().GetCertificate(nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(certificate.Certificate[0]).To(Equal(stored.serving.cert.Raw))

			Expect(mwcBundle(ownWebhookName)).To(Equal(bundle))
			Expect(mwcBundle(foreignWebhookName)).To(Equal(foreignCaBundle))
			Expect(vwcBundle()).To(Equal(bundle))
			Expect(crdBundle(testCrdName)).To(Equal(bundle))
		})

		It("does not write anything in a second pass", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			versions := resourceVersions()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(resourceVersions()).To(Equal(versions))
		})

		It("adopts the material that another replica has stored", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			versions := resourceVersions()

			otherReplica := newTestManager(config, baseClient, &now)
			Expect(otherReplica.runPass(ctx, logger)).To(Succeed())
			Expect(otherReplica.Store().CaBundle()).To(Equal(manager.Store().CaBundle()))
			Expect(resourceVersions()).To(Equal(versions))
		})

		It("restores the secret data if it has been wiped while the secret has been kept", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			secret := getSecret()
			data := secret.Data
			secret.Data = nil
			Expect(baseClient.Update(ctx, secret)).To(Succeed())

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(getSecret().Data).To(Equal(data))
		})

		It("adopts valid material from a recreated secret (for example after a backup restore)", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			data := getSecret().Data
			Expect(baseClient.Delete(ctx, getSecret())).To(Succeed())
			Expect(baseClient.Create(ctx, testSecret("secret-uid-2", data))).To(Succeed())
			versions := resourceVersions()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(resourceVersions()).To(Equal(versions))
		})

		It("generates a new CA if the secret has been recreated, without retaining the CA of the deleted secret", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			previousCa := storedMaterial().activeCa.cert
			Expect(baseClient.Delete(ctx, getSecret())).To(Succeed())
			Expect(baseClient.Create(ctx, testSecret("secret-uid-2", nil))).To(Succeed())

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			stored := storedMaterial()
			Expect(stored.activeCa.cert.Equal(previousCa)).To(BeFalse())
			Expect(parseCertificates(stored.bundle())).To(HaveLen(1))
			Expect(mwcBundle(ownWebhookName)).To(Equal(stored.bundle()))
		})

		It("generates new material if the secret contains invalid data", func() {
			objects[0] = testSecret("secret-uid-1", map[string][]byte{keyCaBundle: []byte("invalid")})
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(storedMaterial().bundle()).To(Equal(manager.Store().CaBundle()))
		})
	})

	Describe("retaining foreign certificates", func() {
		It("retains the CA of older chart versions from the targets and from the previous secret", func() {
			chartCaInBundle := createForeignCa(legacyChartCaCommonName, testNow.Add(300*24*time.Hour))
			chartCaInSecret := createForeignCa("ca-with-another-name", testNow.Add(300*24*time.Hour))
			objects[1] = testMutatingWebhookConfiguration(testNamespace, encodeCertificates([]*x509.Certificate{chartCaInBundle}))
			objects = append(objects, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testPreviousSecretName},
				Type:       corev1.SecretTypeTLS,
				Data:       map[string][]byte{keyCaBundle: encodeCertificates([]*x509.Certificate{chartCaInSecret})},
			})
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			stored := storedMaterial()
			Expect(stored.retainedChartCas).To(HaveLen(2))
			bundle := parseCertificates(mwcBundle(ownWebhookName))
			Expect(bundle).To(HaveLen(3))
			Expect(containsCertificate(bundle, chartCaInBundle)).To(BeTrue())
			Expect(containsCertificate(bundle, chartCaInSecret)).To(BeTrue())
			Expect(crdBundle(testCrdName)).To(Equal(stored.bundle()))
		})

		It("retains the CA of an older chart version that shows up after a rollback while the secret has been kept", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			activeCa := storedMaterial().activeCa.cert

			chartCaInBundle := createForeignCa(legacyChartCaCommonName, testNow.Add(300*24*time.Hour))
			chartCaInSecret := createForeignCa("ca-with-another-name", testNow.Add(300*24*time.Hour))
			mwc := getMwc()
			for i := range mwc.Webhooks {
				if mwc.Webhooks[i].Name == ownWebhookName {
					mwc.Webhooks[i].ClientConfig.CABundle = encodeCertificates([]*x509.Certificate{chartCaInBundle})
				}
			}
			Expect(baseClient.Update(ctx, mwc)).To(Succeed())
			Expect(baseClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testPreviousSecretName},
				Type:       corev1.SecretTypeTLS,
				Data:       map[string][]byte{keyCaBundle: encodeCertificates([]*x509.Certificate{chartCaInSecret})},
			})).To(Succeed())

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			stored := storedMaterial()
			Expect(stored.activeCa.cert.Equal(activeCa)).To(BeTrue())
			Expect(stored.retainedChartCas).To(HaveLen(2))
			bundle := parseCertificates(mwcBundle(ownWebhookName))
			Expect(bundle).To(HaveLen(3))
			Expect(containsCertificate(bundle, chartCaInBundle)).To(BeTrue())
			Expect(containsCertificate(bundle, chartCaInSecret)).To(BeTrue())
			Expect(crdBundle(testCrdName)).To(Equal(stored.bundle()))
		})

		It("removes other foreign certificates only after seven days and once the rollout is complete", func() {
			certManagerCa := createForeignCa("cert-manager-ca", testNow.Add(300*24*time.Hour))
			objects[1] = testMutatingWebhookConfiguration(testNamespace, encodeCertificates([]*x509.Certificate{certManagerCa}))
			objects[4] = testDeployment(false)
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(parseCertificates(mwcBundle(ownWebhookName))).To(HaveLen(2))

			now = testNow.Add(foreignCertRetention + time.Hour)
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(parseCertificates(mwcBundle(ownWebhookName))).To(HaveLen(2))
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonRetentionOverdue)))

			deployment := testDeployment(true)
			Expect(baseClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
			deployment.Status = testDeployment(true).Status
			Expect(baseClient.Status().Update(ctx, deployment)).To(Succeed())

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			bundle := parseCertificates(mwcBundle(ownWebhookName))
			Expect(bundle).To(HaveLen(1))
			Expect(containsCertificate(bundle, certManagerCa)).To(BeFalse())
		})
	})

	Describe("selecting and patching the targets", func() {
		It("ignores targets without the injection label or with the label of another installation", func() {
			objects[1] = testMutatingWebhookConfiguration("", nil)
			objects[3] = testCrd(testCrdName, "other-namespace", nil)
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(mwcBundle(ownWebhookName)).To(BeEmpty())
			Expect(crdBundle(testCrdName)).To(BeEmpty())
			Expect(vwcBundle()).To(Equal(manager.Store().CaBundle()))
			Expect(manager.Store().IsReady()).To(BeTrue())
		})

		It("sets the readiness latch if none of the targets exists", func() {
			objects = []client.Object{testSecret("secret-uid-1", nil), testDeployment(true)}
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(manager.Store().IsReady()).To(BeTrue())
		})

		It("reports a permanent error for a target whose webhooks do not point at the operator", func() {
			objects[2] = testValidatingWebhookConfiguration(foreignServiceName, foreignCaBundle)
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(MatchError(ContainSubstring(testVwcName)))
			Expect(manager.Store().IsReady()).To(BeTrue())
			Expect(vwcBundle()).To(Equal(foreignCaBundle))
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonInjectionFailed)))
		})

		It("sets the readiness latch after a permanent error, but not after a transient one", func() {
			var crdPatchError error
			funcs.Patch = func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if obj.GetName() == testCrdName && crdPatchError != nil {
					return crdPatchError
				}
				return c.Patch(ctx, obj, patch, opts...)
			}
			buildManager()

			crdPatchError = apierrors.NewServiceUnavailable("unavailable")
			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(manager.Store().IsReady()).To(BeFalse())

			crdPatchError = apierrors.NewForbidden(
				schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"},
				testCrdName,
				fmt.Errorf("denied by policy"),
			)
			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(manager.Store().IsReady()).To(BeTrue())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonInjectionFailed)))

			crdPatchError = nil
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(crdBundle(testCrdName)).To(Equal(manager.Store().CaBundle()))
		})

		It("treats a denial by an admission webhook that does not set a status code as a permanent error", func() {
			funcs.Patch = func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if obj.GetName() == testCrdName {
					// what the API server returns for a denial without a status code, for example by Kyverno
					return &apierrors.StatusError{ErrStatus: metav1.Status{
						Status:  metav1.StatusFailure,
						Code:    http.StatusBadRequest,
						Message: `admission webhook "validate.kyverno.svc-fail" denied the request: blocked by policy`,
					}}
				}
				return c.Patch(ctx, obj, patch, opts...)
			}
			buildManager()

			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(manager.Store().IsReady()).To(BeTrue())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonInjectionFailed)))
		})

		It("retries a patch after a conflict", func() {
			mwcPatchAttempts := 0
			funcs.Patch = func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if obj.GetName() == testMwcName {
					mwcPatchAttempts++
					if mwcPatchAttempts == 1 {
						return apierrors.NewConflict(
							schema.GroupResource{Group: "admissionregistration.k8s.io", Resource: "mutatingwebhookconfigurations"},
							testMwcName,
							fmt.Errorf("object was modified"),
						)
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			}
			buildManager()

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(mwcPatchAttempts).To(Equal(2))
			Expect(mwcBundle(ownWebhookName)).To(Equal(manager.Store().CaBundle()))
		})

		It("rejects a patch that is based on a stale read", func() {
			buildManager()

			staleMwc := getMwc()
			originalMwc := staleMwc.DeepCopy()
			concurrentMwc := getMwc()
			concurrentMwc.Webhooks[0].ClientConfig.CABundle = []byte("concurrent change")
			Expect(baseClient.Update(ctx, concurrentMwc)).To(Succeed())
			staleMwc.Webhooks[0].ClientConfig.CABundle = []byte("stale change")
			Expect(apierrors.IsConflict(baseClient.Patch(ctx, staleMwc, patchFor(originalMwc)))).To(BeTrue())

			staleCrd := getCrd(testCrdName)
			originalCrd := staleCrd.DeepCopy()
			concurrentCrd := getCrd(testCrdName)
			concurrentCrd.Spec.Conversion.Webhook.ClientConfig.CABundle = []byte("concurrent change")
			Expect(baseClient.Update(ctx, concurrentCrd)).To(Succeed())
			staleCrd.Spec.Conversion.Webhook.ClientConfig.CABundle = []byte("stale change")
			Expect(apierrors.IsConflict(baseClient.Patch(ctx, staleCrd, patchFor(originalCrd)))).To(BeTrue())
		})
	})

	Describe("storing the material", func() {
		It("does not load anything if the secret does not exist", func() {
			objects = objects[1:]
			buildManager()

			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(manager.Store().IsReady()).To(BeFalse())
			_, err := manager.Store().GetCertificate(nil)
			Expect(err).To(HaveOccurred())
			Expect(mwcBundle(ownWebhookName)).To(BeEmpty())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonSecretMissing)))
		})

		It("does not use material that could not be stored", func() {
			funcs.Update = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, isSecret := obj.(*corev1.Secret); isSecret {
					return apierrors.NewForbidden(
						schema.GroupResource{Resource: "secrets"}, testSecretName, fmt.Errorf("denied by policy"),
					)
				}
				return c.Update(ctx, obj, opts...)
			}
			buildManager()

			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(manager.Store().IsReady()).To(BeFalse())
			Expect(manager.Store().CaBundle()).To(BeNil())
			Expect(mwcBundle(ownWebhookName)).To(BeEmpty())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonSecretUpdateFailed)))
		})

		It("adopts the material of a concurrent writer after a conflict", func() {
			var otherReplica *Manager
			concurrentWriteDone := false
			funcs.Update = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, isSecret := obj.(*corev1.Secret); isSecret && !concurrentWriteDone {
					concurrentWriteDone = true
					Expect(otherReplica.runPass(ctx, logger)).To(Succeed())
				}
				// After the concurrent write, the resource version of obj is outdated and the fake client rejects the update.
				return c.Update(ctx, obj, opts...)
			}
			buildManager()
			otherReplica = newTestManager(config, baseClient, &now)

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(concurrentWriteDone).To(BeTrue())
			Expect(manager.Store().CaBundle()).To(Equal(otherReplica.Store().CaBundle()))
			Expect(storedMaterial().bundle()).To(Equal(manager.Store().CaBundle()))
			Expect(mwcBundle(ownWebhookName)).To(Equal(manager.Store().CaBundle()))
		})
	})

	Describe("rotating the CA", func() {
		rotationStart := testNow.Add(caValidity - caRotationLeadTime + time.Hour)

		servedLeaf := func() *x509.Certificate {
			certificate, err := manager.Store().GetCertificate(nil)
			Expect(err).ToNot(HaveOccurred())
			leaf, err := x509.ParseCertificate(certificate.Certificate[0])
			Expect(err).ToNot(HaveOccurred())
			return leaf
		}

		It("starts a rotation, and switches once all targets carry the new CA and a day has passed", func() {
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			initialCa := storedMaterial().activeCa.cert

			now = rotationStart
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			rotating := storedMaterial()
			Expect(rotating.pendingCa).ToNot(BeNil())
			Expect(rotating.activeCa.cert.Equal(initialCa)).To(BeTrue())
			bundle := rotating.bundle()
			Expect(parseCertificates(bundle)).To(HaveLen(2))
			Expect(mwcBundle(ownWebhookName)).To(Equal(bundle))
			Expect(vwcBundle()).To(Equal(bundle))
			Expect(crdBundle(testCrdName)).To(Equal(bundle))
			Expect(verifyServingCertChain(servedLeaf(), initialCa, now)).To(Succeed())

			now = rotationStart.Add(minimumRotationAge - time.Minute)
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(storedMaterial().pendingCa).ToNot(BeNil())

			targetVersions := resourceVersions()[1:]
			now = rotationStart.Add(minimumRotationAge + time.Minute)
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			switched := storedMaterial()
			Expect(switched.pendingCa).To(BeNil())
			Expect(switched.activeCa.cert.Equal(rotating.pendingCa.cert)).To(BeTrue())
			Expect(switched.bundle()).To(Equal(bundle))
			Expect(manager.Store().CaBundle()).To(Equal(bundle))
			Expect(resourceVersions()[1:]).To(Equal(targetVersions))
			Expect(verifyServingCertChain(servedLeaf(), switched.activeCa.cert, now)).To(Succeed())
		})

		It("blocks the switch while a target does not carry the new CA, and forces it shortly before the CA expires", func() {
			blockCrdPatches := false
			funcs.Patch = func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if obj.GetName() == testCrdName && blockCrdPatches {
					return apierrors.NewForbidden(
						schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"},
						testCrdName,
						fmt.Errorf("denied by policy"),
					)
				}
				return c.Patch(ctx, obj, patch, opts...)
			}
			buildManager()
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			blockCrdPatches = true

			now = rotationStart
			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			now = rotationStart.Add(minimumRotationAge + time.Minute)
			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			Expect(storedMaterial().pendingCa).ToNot(BeNil())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonRotationBlocked)))

			activeCa := storedMaterial().activeCa.cert
			now = activeCa.NotAfter.Add(-forcedSwitchLeadTime + time.Hour)
			Expect(manager.runPass(ctx, logger)).ToNot(Succeed())
			switched := storedMaterial()
			Expect(switched.pendingCa).To(BeNil())
			Expect(switched.activeCa.cert.Equal(activeCa)).To(BeFalse())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(reasonRotationForced)))
			Expect(verifyServingCertChain(servedLeaf(), switched.activeCa.cert, now)).To(Succeed())
		})

		It("waits for the Perses dashboard CRD, and notifies the listeners while it carries an outdated bundle", func() {
			config.PersesCrdName = testPersesCrdName
			objects = append(objects, testCrd(testPersesCrdName, "", nil))
			buildManager()
			notifications := make(chan []byte, 100)
			manager.Store().OnCaBundleChange(func(bundle []byte) {
				notifications <- bundle
			})

			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Eventually(notifications).Should(Receive(Equal(manager.Store().CaBundle())))

			now = rotationStart
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			now = rotationStart.Add(minimumRotationAge + time.Minute)
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(storedMaterial().pendingCa).ToNot(BeNil())
			Expect(receivedEvents()).To(ContainElement(ContainSubstring(testPersesCrdName)))
			Eventually(notifications).Should(Receive(Equal(manager.Store().CaBundle())))

			persesCrd := getCrd(testPersesCrdName)
			persesCrd.Spec.Conversion.Webhook.ClientConfig.CABundle = manager.Store().CaBundle()
			Expect(baseClient.Update(ctx, persesCrd)).To(Succeed())
			Expect(manager.runPass(ctx, logger)).To(Succeed())
			Expect(storedMaterial().pendingCa).To(BeNil())
		})
	})
})

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(appsv1.AddToScheme(scheme)).To(Succeed())
	Expect(admissionregistrationv1.AddToScheme(scheme)).To(Succeed())
	Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

func testConfig() Config {
	return Config{
		OperatorNamespace:  testNamespace,
		SecretName:         testSecretName,
		PreviousSecretName: testPreviousSecretName,
		DeploymentName:     testDeploymentName,
		WebhookServiceName: testServiceName,
		Targets: []Target{
			{Kind: TargetKindMutatingWebhookConfiguration, Name: testMwcName},
			{Kind: TargetKindValidatingWebhookConfiguration, Name: testVwcName},
			{Kind: TargetKindCustomResourceDefinition, Name: testCrdName},
		},
	}
}

func newTestManager(config Config, c client.Client, now *time.Time) *Manager {
	m := NewManager(config, c, NewStore())
	m.now = func() time.Time {
		return *now
	}
	return m
}

func testSecret(uid types.UID, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName, UID: uid},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

func testLabels(labelValue string) map[string]string {
	if labelValue == "" {
		return nil
	}
	return map[string]string{InjectionTargetLabel: labelValue}
}

func testClientConfig(serviceName string, caBundle []byte) admissionregistrationv1.WebhookClientConfig {
	return admissionregistrationv1.WebhookClientConfig{
		Service:  &admissionregistrationv1.ServiceReference{Namespace: testNamespace, Name: serviceName},
		CABundle: caBundle,
	}
}

func testMutatingWebhookConfiguration(
	labelValue string,
	caBundle []byte,
) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testMwcName, Labels: testLabels(labelValue)},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{Name: ownWebhookName, ClientConfig: testClientConfig(testServiceName, caBundle)},
			{Name: foreignWebhookName, ClientConfig: testClientConfig(foreignServiceName, foreignCaBundle)},
		},
	}
}

func testValidatingWebhookConfiguration(
	serviceName string,
	caBundle []byte,
) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testVwcName, Labels: testLabels(testNamespace)},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{Name: ownWebhookName, ClientConfig: testClientConfig(serviceName, caBundle)},
		},
	}
}

func testCrd(name string, labelValue string, caBundle []byte) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: testLabels(labelValue)},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Conversion: &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service:  &apiextensionsv1.ServiceReference{Namespace: testNamespace, Name: testServiceName},
						CABundle: caBundle,
					},
					ConversionReviewVersions: []string{"v1"},
				},
			},
		},
	}
}

func testDeployment(rolloutComplete bool) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testDeploymentName, Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           2,
			UpdatedReplicas:    2,
			AvailableReplicas:  2,
		},
	}
	if !rolloutComplete {
		// one replica of the previous revision is still running
		deployment.Status.Replicas = 3
	}
	return deployment
}
