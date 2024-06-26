// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	kubeContextForTest            = "docker-desktop"
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"
)

var (
	certManagerHasBeenInstalled = false

	originalKubeContext       string
	kubeContextHasBeenChanged bool

	setupFinishedSuccessfully bool

	buildOperatorControllerImageFromLocalSources = true
	buildInstrumentationImageFromLocalSources    = true
	operatorHelmChart                            = localHelmChart
	operatorHelmChartUrl                         = ""
	operatorNamespace                            = "dash0-system"
	images                                       = Images{
		operator: ImageSpec{
			repository: "operator-controller",
			tag:        "latest",
			pullPolicy: "Never",
		},
		instrumentation: ImageSpec{
			repository: "instrumentation",
			tag:        "latest",
			pullPolicy: "Never",
		},
	}
)

var _ = Describe("Dash0 Kubernetes Operator", Ordered, func() {

	BeforeAll(func() {
		pwdOutput, err := Run(exec.Command("pwd"), false)
		Expect(err).NotTo(HaveOccurred())
		workingDir := strings.TrimSpace(pwdOutput)
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		CheckIfRequiredPortsAreBlocked()
		RenderTemplates()
		kubeContextHasBeenChanged, originalKubeContext = SetKubeContext(kubeContextForTest)

		certManagerHasBeenInstalled = EnsureCertManagerIsInstalled()
		RecreateNamespace(applicationUnderTestNamespace)

		readAndApplyEnvironmentVariables()
		RebuildOperatorControllerImage(images.operator, buildOperatorControllerImageFromLocalSources)
		RebuildDash0InstrumentationImage(images.instrumentation, buildInstrumentationImageFromLocalSources)
		RebuildNodeJsApplicationContainerImage()

		setupFinishedSuccessfully = true
	})

	AfterAll(func() {
		UninstallCertManagerIfApplicable(certManagerHasBeenInstalled)

		if applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}
		if kubeContextHasBeenChanged {
			RevertKubeCtx(originalKubeContext)
		}
	})

	BeforeEach(func() {
		DeleteTestIdFiles()
	})

	AfterEach(func() {
		if setupFinishedSuccessfully {
			// As an alternative to undeploying all applications under test (deployment, daemonset, cronjob, ...) we
			// could also delete the whole namespace for the application under test to after each test case get rid of
			// all applications (and then recreate the namespace before each test). However, this would mean we also
			// need to deploy the OpenTelemetry collector to the target namespace again for each test case, which would
			// slow down tests a bit more.
			RemoveAllTestApplications(applicationUnderTestNamespace)
			DeleteTestIdFiles()
		}
	})

	Describe("controller", func() {
		AfterEach(func() {
			UndeployDash0CustomResource(applicationUnderTestNamespace)
			UndeployOperatorAndCollector(operatorNamespace)
		})

		type controllerTestWorkloadConfig struct {
			workloadType    string
			port            int
			installWorkload func(string) error
			isBatch         bool
		}

		workloadConfigs := []controllerTestWorkloadConfig{
			{
				workloadType:    "cronjob",
				port:            1205,
				installWorkload: InstallNodeJsCronJob,
				isBatch:         true,
			}, {
				workloadType:    "daemonset",
				port:            1206,
				installWorkload: InstallNodeJsDaemonSet,
			}, {
				workloadType:    "deployment",
				port:            1207,
				installWorkload: InstallNodeJsDeployment,
			}, {
				workloadType:    "replicaset",
				port:            1209,
				installWorkload: InstallNodeJsReplicaSet,
			}, {
				workloadType:    "statefulset",
				port:            1210,
				installWorkload: InstallNodeJsStatefulSet,
			},
		}

		Describe("when instrumenting existing workloads", func() {
			It("should instrument and uninstrument all workload types", func() {
				By("deploying all workloads")
				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					By(fmt.Sprintf("deploying the Node.js %s", config.workloadType))
					Expect(config.installWorkload(applicationUnderTestNamespace)).To(Succeed())
				})

				By("deploy the operator and the Dash0 custom resource")
				DeployOperatorWithCollectorAndClearExportedTelemetry(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
				)
				DeployDash0CustomResource(applicationUnderTestNamespace)

				testIds := make(map[string]string)
				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller", config.workloadType))
					testIds[config.workloadType] = VerifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						config.workloadType,
						config.port,
						config.isBatch,
						images,
						"controller",
					)
				})

				UndeployDash0CustomResource(applicationUnderTestNamespace)

				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					VerifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						config.workloadType,
						config.port,
						config.isBatch,
						testIds[config.workloadType],
						"controller",
					)
				})
			})
		})

		Describe("when it detects existing jobs or ownerless pods", func() {
			It("should label immutable jobs accordingly", func() {
				By("installing the Node.js job")
				Expect(InstallNodeJsJob(applicationUnderTestNamespace)).To(Succeed())
				By("deploy the operator and the Dash0 custom resource")
				DeployOperatorWithCollectorAndClearExportedTelemetry(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
				)
				DeployDash0CustomResource(applicationUnderTestNamespace)
				By("verifying that the Node.js job has been labelled by the controller and that an event has been emitted")
				Eventually(func(g Gomega) {
					VerifyLabels(g, applicationUnderTestNamespace, "job", false, images, "controller")
					VerifyFailedInstrumentationEvent(
						g,
						applicationUnderTestNamespace,
						"job",
						"Dash0 instrumentation of this workload by the controller has not been successful. "+
							"Error message: Dash0 cannot instrument the existing job "+
							"e2e-application-under-test-namespace/dash0-operator-nodejs-20-express-test-job, since "+
							"this type of workload is immutable.",
					)
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())

				UndeployDash0CustomResource(applicationUnderTestNamespace)

				VerifyThatFailedInstrumentationAttemptLabelsHaveBeenRemovedRemoved(applicationUnderTestNamespace, "job")
			})

			It("should ignore existing pods", func() {
				By("installing the Node.js pod")
				Expect(InstallNodeJsPod(applicationUnderTestNamespace)).To(Succeed())
				By("deploy the operator and the Dash0 custom resource")
				DeployOperatorWithCollectorAndClearExportedTelemetry(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
				)
				DeployDash0CustomResource(applicationUnderTestNamespace)
				By("verifying that the Node.js pod has not been labelled")
				Eventually(func(g Gomega) {
					VerifyNoDash0Labels(g, applicationUnderTestNamespace, "pod")
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
			})
		})
	})

	Describe("webhook", func() {
		BeforeAll(func() {
			DeployOperatorWithCollectorAndClearExportedTelemetry(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
			)

			fmt.Fprint(GinkgoWriter, "waiting 10 seconds to give the webhook some time to get ready\n")
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			UndeployOperatorAndCollector(operatorNamespace)
		})

		BeforeEach(func() {
			DeployDash0CustomResource(applicationUnderTestNamespace)
		})

		AfterEach(func() {
			UndeployDash0CustomResource(applicationUnderTestNamespace)
		})

		type webhookTest struct {
			workloadType    string
			port            int
			installWorkload func(string) error
			isBatch         bool
		}

		DescribeTable(
			"when instrumenting new workloads",
			func(config webhookTest) {
				By(fmt.Sprintf("installing the Node.js %s", config.workloadType))
				Expect(config.installWorkload(applicationUnderTestNamespace)).To(Succeed())
				By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the webhook", config.workloadType))
				VerifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					config.workloadType,
					config.port,
					config.isBatch,
					images,
					"webhook",
				)

				if config.workloadType == "job" {
					// For all other workload types, reverting the instrumentation is tested by the controller test
					// suite. But the controller cannot instrument jobs, so we cannot test the (failing)
					// uninstrumentation procedure there. Thus, for jobs, we test the failing uninstrumentation and
					// its effects here.
					By("verifying that removing the Dash0 custom resource attempts to uninstruments the job")
					UndeployDash0CustomResource(applicationUnderTestNamespace)

					Eventually(func(g Gomega) {
						// Verify that the instrumentation labels are still in place -- since we cannot undo the
						// instrumentation, the labels must also not be removed.
						By("verifying that the job still has labels")
						VerifyLabels(g, applicationUnderTestNamespace, config.workloadType, true, images, "webhook")

						By("verifying failed uninstrumentation event")
						VerifyFailedUninstrumentationEvent(
							g,
							applicationUnderTestNamespace,
							"job",
							"The controller's attempt to remove the Dash0 instrumentation from this workload has not "+
								"been successful. Error message: Dash0 cannot remove the instrumentation from the "+
								"existing job "+
								"e2e-application-under-test-namespace/dash0-operator-nodejs-20-express-test-job, since "+
								"this type of workload is immutable.",
						)
					}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
				}
			},
			Entry("should instrument new cron jobs", webhookTest{
				workloadType:    "cronjob",
				port:            1205,
				installWorkload: InstallNodeJsCronJob,
				isBatch:         true,
			}),
			Entry("should instrument new daemon sets", webhookTest{
				workloadType:    "daemonset",
				port:            1206,
				installWorkload: InstallNodeJsDaemonSet,
			}),
			Entry("should instrument new deployments", webhookTest{
				workloadType:    "deployment",
				port:            1207,
				installWorkload: InstallNodeJsDeployment,
			}),
			Entry("should instrument new jobs", webhookTest{
				workloadType:    "job",
				port:            1208,
				installWorkload: InstallNodeJsJob,
				isBatch:         true,
			}),
			Entry("should instrument new pods", webhookTest{
				workloadType:    "pod",
				port:            1211,
				installWorkload: InstallNodeJsPod,
			}),
			Entry("should instrument new replica sets", webhookTest{
				workloadType:    "replicaset",
				port:            1209,
				installWorkload: InstallNodeJsReplicaSet,
			}),
			Entry("should instrument new stateful sets", webhookTest{
				workloadType:    "statefulset",
				port:            1210,
				installWorkload: InstallNodeJsStatefulSet,
			}),
		)
	})

	Describe("operator removal", func() {

		const (
			namespace1 = "e2e-application-under-test-namespace-removal-1"
			namespace2 = "e2e-application-under-test-namespace-removal-2"
		)

		BeforeAll(func() {
			By("creating test namespaces")
			RecreateNamespace(namespace1)
			RecreateNamespace(namespace2)
		})

		AfterEach(func() {
			UndeployDash0CustomResource(namespace1)
			UndeployDash0CustomResource(namespace2)
			UndeployOperatorAndCollector(operatorNamespace)
		})

		AfterAll(func() {
			By("removing test namespaces")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace1))
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace2))
			UndeployOperatorAndCollector(operatorNamespace)
		})

		type removalTestNamespaceConfig struct {
			namespace       string
			workloadType    string
			port            int
			installWorkload func(string) error
		}

		configs := []removalTestNamespaceConfig{
			{
				namespace:       "e2e-application-under-test-namespace-removal-1",
				workloadType:    "daemonset",
				port:            1206,
				installWorkload: InstallNodeJsDaemonSet,
			},
			{
				namespace:       "e2e-application-under-test-namespace-removal-2",
				workloadType:    "deployment",
				port:            1207,
				installWorkload: InstallNodeJsDeployment,
			},
		}

		Describe("when uninstalling the operator via helm", func() {
			It("should remove all Dash0 custom resources and uninstrument all workloads", func() {
				By("deploying workloads")
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("deploying the Node.js %s to namespace %s", config.workloadType, config.namespace))
					Expect(config.installWorkload(config.namespace)).To(Succeed())
				})

				By("deploy the operator and the Dash0 custom resource")
				DeployOperatorWithCollectorAndClearExportedTelemetry(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
				)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					DeployDash0CustomResource(config.namespace)
				})

				testIds := make(map[string]string)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller", config.workloadType))
					testIds[config.workloadType] = VerifyThatWorkloadHasBeenInstrumented(
						config.namespace,
						config.workloadType,
						config.port,
						false,
						images,
						"controller",
					)
				})

				UndeployOperatorAndCollector(operatorNamespace)

				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					VerifyThatInstrumentationHasBeenReverted(
						config.namespace,
						config.workloadType,
						config.port,
						false,
						testIds[config.workloadType],
						"controller",
					)
				})

				Eventually(func(g Gomega) {
					for _, config := range configs {
						VerifyDash0CustomResourceDoesNotExist(g, config.namespace)
					}
					VerifyDash0OperatorReleaseIsNotInstalled(g, operatorNamespace)
				}).Should(Succeed())
			})
		})
	})
})

func runInParallelForAllWorkloadTypes[C any](
	workloadConfigs []C,
	testStep func(C),
) {
	var wg sync.WaitGroup
	for _, config := range workloadConfigs {
		wg.Add(1)
		go func(cfg C) {
			defer GinkgoRecover()
			defer wg.Done()
			testStep(cfg)
		}(config)
	}
	wg.Wait()
}

func readAndApplyEnvironmentVariables() {
	buildOperatorControllerImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_OPERATOR_CONTROLLER_IMAGE", "")
	var err error
	if buildOperatorControllerImageFromLocalSourcesRaw != "" {
		buildOperatorControllerImageFromLocalSources, err = strconv.ParseBool(buildOperatorControllerImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
	buildInstrumentationImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_INSTRUMENTATION_IMAGE", "")
	if buildInstrumentationImageFromLocalSourcesRaw != "" {
		buildInstrumentationImageFromLocalSources, err = strconv.ParseBool(buildInstrumentationImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)
	images.operator.repository = getEnvOrDefault("IMG_REPOSITORY", images.operator.repository)
	images.operator.tag = getEnvOrDefault("IMG_TAG", images.operator.tag)
	images.operator.digest = getEnvOrDefault("IMG_DIGEST", images.operator.digest)
	images.operator.pullPolicy = getEnvOrDefault("IMG_PULL_POLICY", images.operator.pullPolicy)
	images.instrumentation.repository = getEnvOrDefault(
		"INSTRUMENTATION_IMG_REPOSITORY",
		images.instrumentation.repository,
	)
	images.instrumentation.tag = getEnvOrDefault("INSTRUMENTATION_IMG_TAG", images.instrumentation.tag)
	images.instrumentation.digest = getEnvOrDefault("INSTRUMENTATION_IMG_DIGEST", images.instrumentation.digest)
	images.instrumentation.pullPolicy = getEnvOrDefault(
		"INSTRUMENTATION_IMG_PULL_POLICY",
		images.instrumentation.pullPolicy,
	)
}

func getEnvOrDefault(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)
	if isSet {
		return value
	}
	return defaultValue
}
