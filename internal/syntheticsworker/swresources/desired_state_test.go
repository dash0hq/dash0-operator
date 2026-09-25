// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package swresources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testOperatorNamespace = "dash0-system"
	testNamePrefix        = "dash0-operator-test"
	testImage             = "ghcr.io/dash0hq/dash0-synthetics-worker:1.2.3"
)

var (
	testAuthTokenEnvVar = &corev1.EnvVar{
		Name:  authTokenEnvVarName,
		Value: "dummy-token",
	}

	testSpec = dash0v1alpha1.SyntheticsWorkerInstance{
		LocationID: "test-location",
	}
)

func testConfig() *util.SyntheticsWorkerConfig {
	return &util.SyntheticsWorkerConfig{
		OperatorNamespace: testOperatorNamespace,
		NamePrefix:        testNamePrefix,
		ServerAddress:     SyntheticsWorkerServerAddress,
		Images: util.Images{
			SyntheticsWorkerImage:           testImage,
			SyntheticsWorkerImagePullPolicy: corev1.PullAlways,
		},
	}
}

var _ = Describe("The desired state of the synthetics-worker resources", func() {
	It("renders exactly the expected set of resources with the expected names", func() {
		desiredState := assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar)

		Expect(desiredState).To(HaveLen(2))
		Expect(getServiceAccount(desiredState).Name).To(Equal(testNamePrefix + "-synthetics-worker-test-location-sa"))
		Expect(getDeployment(desiredState).Name).To(Equal(testNamePrefix + "-synthetics-worker-test-location"))
	})

	It("deploys the resources into the operator namespace", func() {
		desiredState := assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar)

		Expect(getServiceAccount(desiredState).Namespace).To(Equal(testOperatorNamespace))
		Expect(getDeployment(desiredState).Namespace).To(Equal(testOperatorNamespace))
	})

	It("adds the ArgoCD prune/compare annotations to all resources", func() {
		desiredState := assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar)

		for _, wrapper := range desiredState {
			annotations := wrapper.object.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/sync-options", "Prune=false"))
			Expect(annotations).To(HaveKeyWithValue("argocd.argoproj.io/compare-options", "IgnoreExtraneous"))
		}
	})

	Describe("the deployment", func() {
		It("uses the configured image, pull policy, and service account", func() {
			desiredState := assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar)
			deployment := getDeployment(desiredState)

			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(getServiceAccount(desiredState).Name))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(testImage))
			Expect(container.ImagePullPolicy).To(Equal(corev1.PullAlways))
		})

		It("uses the configured number of replicas", func() {
			spec := testSpec
			spec.Replicas = ptr.To(int32(3))
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), spec, testAuthTokenEnvVar),
			)
			Expect(*container.Spec.Replicas).To(Equal(int32(3)))
		})

		It("passes the server address as the SYNTHETICS_ADDRESS environment variable", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "SYNTHETICS_ADDRESS", Value: SyntheticsWorkerServerAddress}))
		})

		It("passes the private location ID as the SYNTHETICS_LOCATIONID environment variable", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "SYNTHETICS_LOCATIONID", Value: "test-location"}))
		})

		It("sets LISTENADDRESS to the fixed probe port", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "LISTENADDRESS", Value: ":8011"}))
		})

		It("sets LOGLEVEL to info when development mode is disabled", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "LOGLEVEL", Value: "info"}))
		})

		It("sets LOGLEVEL to debug when development mode is enabled", func() {
			config := testConfig()
			config.DevelopmentMode = true
			container := getDeployment(
				assembleDesiredStateOrFail(config, testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "LOGLEVEL", Value: "debug"}))
		})

		It("does not set SYNTHETICS_INSECURE by default", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal("SYNTHETICS_INSECURE"))
			}
		})

		It("sets SYNTHETICS_INSECURE when TLS is disabled", func() {
			config := testConfig()
			config.Insecure = true
			container := getDeployment(
				assembleDesiredStateOrFail(config, testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "SYNTHETICS_INSECURE", Value: "true"}))
		})

		It("builds SYNTHETICS_HEADERS from the auth token env var, referencing it via $(VAR_NAME)", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(*testAuthTokenEnvVar))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: headersEnvVarName, Value: "authorization=Bearer $(DASH0_SYNTHETICS_WORKER_AUTH_TOKEN)"}))

			// The auth token env var must be defined before SYNTHETICS_HEADERS references it, otherwise Kubernetes
			// cannot expand the "$(VAR_NAME)" reference.
			tokenIdx := indexOfEnvVar(container.Env, authTokenEnvVarName)
			headersIdx := indexOfEnvVar(container.Env, headersEnvVarName)
			Expect(tokenIdx).To(BeNumerically(">=", 0))
			Expect(headersIdx).To(BeNumerically(">", tokenIdx))
		})

		It("does not set DASH0_SYNTHETICS_WORKER_AUTH_TOKEN or SYNTHETICS_HEADERS when no authorization is configured", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, nil),
			).Spec.Template.Spec.Containers[0]
			for _, envVar := range container.Env {
				Expect(envVar.Name).ToNot(Equal(authTokenEnvVarName))
				Expect(envVar.Name).ToNot(Equal(headersEnvVarName))
			}
		})

		It("renders the gRPC liveness and readiness probes on the fixed probe port", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]

			Expect(container.LivenessProbe).ToNot(BeNil())
			Expect(container.LivenessProbe.GRPC).ToNot(BeNil())
			Expect(container.LivenessProbe.GRPC.Port).To(Equal(int32(8011)))
			Expect(*container.LivenessProbe.GRPC.Service).To(Equal("liveness"))

			Expect(container.ReadinessProbe).ToNot(BeNil())
			Expect(container.ReadinessProbe.GRPC).ToNot(BeNil())
			Expect(container.ReadinessProbe.GRPC.Port).To(Equal(int32(8011)))
			Expect(*container.ReadinessProbe.GRPC.Service).To(Equal("readiness"))
		})

		It("applies the resource requirements from the CRD resource, if set", func() {
			spec := testSpec
			spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("32Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			}
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), spec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]

			Expect(container.Resources.Requests.Memory().String()).To(Equal("32Mi"))
			Expect(container.Resources.Limits.Cpu().String()).To(Equal("500m"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("256Mi"))
		})

		It("leaves the container resources unset when the CRD resource does not configure any", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			Expect(container.Resources).To(Equal(corev1.ResourceRequirements{}))
		})

		It("applies a restrictive container security context", func() {
			container := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.Containers[0]
			sc := container.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*sc.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(sc.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("applies a restrictive pod security context", func() {
			podSpec := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec
			sc := podSpec.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(*sc.RunAsUser).To(Equal(int64(65532)))
			Expect(*sc.RunAsGroup).To(Equal(int64(0)))
			Expect(sc.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		})

		It("omits the pod-level runAsUser/runAsGroup on OpenShift so the SCC can assign an in-range UID", func() {
			cfg := testConfig()
			cfg.IsOpenShift = true
			sc := getDeployment(
				assembleDesiredStateOrFail(cfg, testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec.SecurityContext
			Expect(sc).ToNot(BeNil())
			Expect(*sc.RunAsNonRoot).To(BeTrue())
			Expect(sc.RunAsUser).To(BeNil())
			Expect(sc.RunAsGroup).To(BeNil())
		})

		It("does not mount any volume", func() {
			podSpec := getDeployment(
				assembleDesiredStateOrFail(testConfig(), testSpec, testAuthTokenEnvVar),
			).Spec.Template.Spec
			Expect(podSpec.Volumes).To(BeEmpty())
			Expect(podSpec.Containers[0].VolumeMounts).To(BeEmpty())
		})

		Describe("self-monitoring", func() {
			It("sets the OTLP export environment variables when self-monitoring is enabled", func() {
				container := deploymentContainer(selfMonitoringInputWithDash0Export())

				Expect(container.Env).To(ContainElement(corev1.EnvVar{
					Name:  "SELF_MONITORING_AUTH_TOKEN",
					Value: AuthorizationTokenTest,
				}))
				Expect(container.Env).To(ContainElement(corev1.EnvVar{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: EndpointDash0WithProtocolTest,
				}))
				Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"}))
			})

			It("keeps a secret ref for the self-monitoring auth token instead of resolving it into the pod spec", func() {
				container := deploymentContainer(selfMonitoringInputWithDash0SecretRefExport())

				tokenEnvVar := util.GetEnvVar(&container, "SELF_MONITORING_AUTH_TOKEN")
				Expect(tokenEnvVar).ToNot(BeNil())
				Expect(tokenEnvVar.Value).To(BeEmpty())
				Expect(tokenEnvVar.ValueFrom).ToNot(BeNil())
				Expect(tokenEnvVar.ValueFrom.SecretKeyRef).ToNot(BeNil())
				Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal(SecretRefTest.Name))
				Expect(tokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal(SecretRefTest.Key))
			})

			It("does not set the OTLP export environment variables when self-monitoring is disabled", func() {
				container := deploymentContainer(selfMonitoringInput{})

				for _, envVar := range container.Env {
					Expect(envVar.Name).ToNot(HavePrefix("OTEL_"))
					Expect(envVar.Name).ToNot(Equal("SELF_MONITORING_AUTH_TOKEN"))
				}
			})
		})
	})
})

// deploymentContainer assembles the desired state for the given self-monitoring input and returns the
// synthetics-worker container of the deployment.
func deploymentContainer(selfMonitoring selfMonitoringInput) corev1.Container {
	GinkgoHelper()
	config := testConfig()
	config.Images.OperatorImage = OperatorImageTest
	desiredState, err := assembleDesiredState(
		config,
		testSpec,
		testAuthTokenEnvVar,
		selfMonitoring,
	)
	Expect(err).ToNot(HaveOccurred())
	containers := getDeployment(desiredState).Spec.Template.Spec.Containers
	Expect(containers).To(HaveLen(1))
	return containers[0]
}

func selfMonitoringInputWithDash0Export() selfMonitoringInput {
	return selfMonitoringInput{
		configuration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
			SelfMonitoringEnabled: true,
			Export: dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint:      EndpointDash0Test,
					Authorization: dash0common.Authorization{Token: &AuthorizationTokenTest},
				},
			},
		},
	}
}

func selfMonitoringInputWithDash0SecretRefExport() selfMonitoringInput {
	return selfMonitoringInput{
		configuration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
			SelfMonitoringEnabled: true,
			Export: dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint:      EndpointDash0Test,
					Authorization: dash0common.Authorization{SecretRef: &SecretRefTest},
				},
			},
		},
	}
}

func indexOfEnvVar(envVars []corev1.EnvVar, name string) int {
	for i, envVar := range envVars {
		if envVar.Name == name {
			return i
		}
	}
	return -1
}

func assembleDesiredStateOrFail(
	config *util.SyntheticsWorkerConfig,
	spec dash0v1alpha1.SyntheticsWorkerInstance,
	authTokenEnvVar *corev1.EnvVar,
) []clientObject {
	GinkgoHelper()
	desiredState, err := assembleDesiredState(
		config,
		spec,
		authTokenEnvVar,
		selfMonitoringInput{},
	)
	Expect(err).ToNot(HaveOccurred())
	return desiredState
}

func findObject[T client.Object](desiredState []clientObject) T {
	GinkgoHelper()
	for _, wrapper := range desiredState {
		if typed, ok := wrapper.object.(T); ok {
			return typed
		}
	}
	Fail("could not find the expected object in the desired state")
	var zero T
	return zero
}

func getServiceAccount(desiredState []clientObject) *corev1.ServiceAccount {
	return findObject[*corev1.ServiceAccount](desiredState)
}

func getDeployment(desiredState []clientObject) *appsv1.Deployment {
	return findObject[*appsv1.Deployment](desiredState)
}
