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

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

const (
	dotEnvFile = "test-resources/.env"

	applicationUnderTestNamespace = "e2e-application-under-test-namespace"
)

var (
	workingDir string

	originalKubeContext       string
	kubeContextHasBeenChanged bool

	setupFinishedSuccessfully bool

	buildOperatorControllerImageFromLocalSources    = true
	buildInstrumentationImageFromLocalSources       = true
	buildCollectorImageFromLocalSources             = true
	buildConfigurationReloaderImageFromLocalSources = true
	operatorHelmChart                               = localHelmChart
	operatorHelmChartUrl                            = ""
	operatorNamespace                               = "dash0-system"
	images                                          = Images{
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
		collector: ImageSpec{
			repository: "collector",
			tag:        "latest",
			pullPolicy: "Never",
		},
		configurationReloader: ImageSpec{
			repository: "configuration-reloader",
			tag:        "latest",
			pullPolicy: "Never",
		},
	}
)

var _ = Describe("Dash0 Kubernetes Operator", Ordered, func() {

	BeforeAll(func() {
		// Do not truncate string diff output.
		format.MaxLength = 0

		pwdOutput, err := run(exec.Command("pwd"), false)
		Expect(err).NotTo(HaveOccurred())
		workingDir = strings.TrimSpace(pwdOutput)
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		env, err := readDotEnvFile(dotEnvFile)
		Expect(err).NotTo(HaveOccurred())

		localKubeCtx := env["LOCAL_KUBECTX"]
		if localKubeCtx == "" {
			Fail(fmt.Sprintf("The mandatory setting LOCAL_KUBECTX is missing in the file %s.", dotEnvFile))
		}

		kubeContextHasBeenChanged, originalKubeContext = setKubeContext(localKubeCtx)
		checkIfRequiredPortsAreBlocked()
		renderTemplates()

		recreateNamespace(applicationUnderTestNamespace)

		readAndApplyEnvironmentVariables()
		rebuildOperatorControllerImage(images.operator, buildOperatorControllerImageFromLocalSources)
		rebuildInstrumentationImage(images.instrumentation, buildInstrumentationImageFromLocalSources)
		rebuildCollectorImage(images.collector, buildCollectorImageFromLocalSources)
		rebuildConfigurationReloaderImage(images.configurationReloader, buildConfigurationReloaderImageFromLocalSources)
		rebuildNodeJsApplicationContainerImage()

		setupFinishedSuccessfully = true
	})

	AfterAll(func() {
		if applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}
		if kubeContextHasBeenChanged {
			revertKubeCtx(originalKubeContext)
		}
	})

	BeforeEach(func() {
		deleteTestIdFiles()
		deployOtlpSink(workingDir)
	})

	AfterEach(func() {
		if setupFinishedSuccessfully {
			// As an alternative to undeploying all applications under test (deployment, daemonset, cronjob, ...) we
			// could also delete the whole namespace for the application under test after each test case to get rid of
			// all applications (and then recreate the namespace before each test).
			removeAllTestApplications(applicationUnderTestNamespace)
			uninstallOtlpSink(workingDir)
			deleteTestIdFiles()
		}
	})

	Describe("controller", func() {
		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployOperator(operatorNamespace)
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
				installWorkload: installNodeJsCronJob,
				isBatch:         true,
			}, {
				workloadType:    "daemonset",
				port:            1206,
				installWorkload: installNodeJsDaemonSet,
			}, {
				workloadType:    "deployment",
				port:            1207,
				installWorkload: installNodeJsDeployment,
			}, {
				workloadType:    "replicaset",
				port:            1209,
				installWorkload: installNodeJsReplicaSet,
			}, {
				workloadType:    "statefulset",
				port:            1210,
				installWorkload: installNodeJsStatefulSet,
			},
		}

		Describe("when instrumenting existing workloads", func() {
			It("should instrument and uninstrument all workload types", func() {
				By("deploying all workloads")
				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					By(fmt.Sprintf("deploying the Node.js %s", config.workloadType))
					Expect(config.installWorkload(applicationUnderTestNamespace)).To(Succeed())
				})

				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)
				deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)

				testIds := make(map[string]string)
				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller", config.workloadType))
					testIds[config.workloadType] = verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						config.workloadType,
						config.port,
						config.isBatch,
						images,
						"controller",
					)
				})

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				runInParallelForAllWorkloadTypes(workloadConfigs, func(config controllerTestWorkloadConfig) {
					verifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						config.workloadType,
						config.port,
						config.isBatch,
						testIds[config.workloadType],
						"controller",
					)
				})

				verifyThatCollectorHasBeenRemoved(operatorNamespace)
			})
		})

		Describe("when it detects existing jobs or ownerless pods", func() {
			It("should label immutable jobs accordingly", func() {
				By("installing the Node.js job")
				Expect(installNodeJsJob(applicationUnderTestNamespace)).To(Succeed())
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)
				deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)
				By("verifying that the Node.js job has been labelled by the controller and that an event has been emitted")
				Eventually(func(g Gomega) {
					verifyLabels(g, applicationUnderTestNamespace, "job", false, images, "controller")
					verifyFailedInstrumentationEvent(
						g,
						applicationUnderTestNamespace,
						"job",
						"Dash0 instrumentation of this workload by the controller has not been successful. "+
							"Error message: Dash0 cannot instrument the existing job "+
							"e2e-application-under-test-namespace/dash0-operator-nodejs-20-express-test-job, since "+
							"this type of workload is immutable.",
					)
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				verifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(applicationUnderTestNamespace, "job")
			})

			It("should ignore existing pods", func() {
				By("installing the Node.js pod")
				Expect(installNodeJsPod(applicationUnderTestNamespace)).To(Succeed())
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)
				deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)
				By("verifying that the Node.js pod has not been labelled")
				Eventually(func(g Gomega) {
					verifyNoDash0Labels(g, applicationUnderTestNamespace, "pod")
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
			})
		})

		Describe("when updating workloads at startup", func() {
			It("should update instrumentation modifications at startup", func() {
				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				initialImages := Images{
					operator: ImageSpec{
						repository: "operator-controller",
						tag:        additionalImageTag,
						pullPolicy: "Never",
					},
					instrumentation: ImageSpec{
						repository: "instrumentation",
						tag:        additionalImageTag,
						pullPolicy: "Never",
					},
					collector: ImageSpec{
						repository: "collector",
						tag:        additionalImageTag,
						pullPolicy: "Never",
					},
					configurationReloader: ImageSpec{
						repository: "configuration-reloader",
						tag:        additionalImageTag,
						pullPolicy: "Never",
					},
				}
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, initialImages, false)
				deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)

				By("verifying that the Node.js deployment has been instrumented by the controller")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					1207,
					false,
					initialImages,
					"controller",
				)

				upgradeOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					// now we use :latest instead of :e2e-test to trigger an actual change
					images,
					false,
				)

				By("verifying that the Node.js deployment's instrumentation settings have been updated by the controller")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					1207,
					false,
					// check that the new image tags have been applied to the workload
					images,
					"controller",
				)
			})
		})
	})

	Describe("webhook", func() {
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)

			fmt.Fprint(GinkgoWriter, "waiting 10 seconds to give the webhook some time to get ready\n")
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		BeforeEach(func() {
			deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
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
				verifyThatWorkloadHasBeenInstrumented(
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
					By("verifying that removing the Dash0 monitoring resource attempts to uninstruments the job")
					undeployDash0MonitoringResource(applicationUnderTestNamespace)

					Eventually(func(g Gomega) {
						// Verify that the instrumentation labels are still in place -- since we cannot undo the
						// instrumentation, the labels must also not be removed.
						By("verifying that the job still has labels")
						verifyLabels(g, applicationUnderTestNamespace, config.workloadType, true, images, "webhook")

						By("verifying failed uninstrumentation event")
						verifyFailedUninstrumentationEvent(
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
				installWorkload: installNodeJsCronJob,
				isBatch:         true,
			}),
			Entry("should instrument new daemon sets", webhookTest{
				workloadType:    "daemonset",
				port:            1206,
				installWorkload: installNodeJsDaemonSet,
			}),
			Entry("should instrument new deployments", webhookTest{
				workloadType:    "deployment",
				port:            1207,
				installWorkload: installNodeJsDeployment,
			}),
			Entry("should instrument new jobs", webhookTest{
				workloadType:    "job",
				port:            1208,
				installWorkload: installNodeJsJob,
				isBatch:         true,
			}),
			Entry("should instrument new pods", webhookTest{
				workloadType:    "pod",
				port:            1211,
				installWorkload: installNodeJsPod,
			}),
			Entry("should instrument new replica sets", webhookTest{
				workloadType:    "replicaset",
				port:            1209,
				installWorkload: installNodeJsReplicaSet,
			}),
			Entry("should instrument new stateful sets", webhookTest{
				workloadType:    "statefulset",
				port:            1210,
				installWorkload: installNodeJsStatefulSet,
			}),
		)

		It("should revert an instrumented workload when the opt-out label is added after the fact", func() {
			By("installing the Node.js deployment")
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			testId := verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				images,
				"webhook",
			)

			By("Adding the opt-out label to the deployment")
			Expect(addOptOutLabel(
				applicationUnderTestNamespace,
				"deployment",
				"dash0-operator-nodejs-20-express-test-deployment",
			)).To(Succeed())

			verifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				testId,
				"webhook",
			)
		})

		It("should instrument a workload when the opt-out label is removed from it", func() {
			By("installing the Node.js daemonset with dash0.com/enable=false")
			Expect(installNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js daemonset has not been instrumented by the webhook")
			Consistently(func(g Gomega) {
				verifyNoDash0LabelsOrOnlyOptOut(g, applicationUnderTestNamespace, "daemonset", true)
			}, 10*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

			By("Removing the opt-out label from the daemonset")
			Expect(removeOptOutLabel(
				applicationUnderTestNamespace,
				"daemonset",
				"dash0-operator-nodejs-20-express-test-daemonset",
			)).To(Succeed())

			By("verifying that the Node.js daemonset has been instrumented by the webhook")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"daemonset",
				1206,
				false,
				images,
				"webhook",
			)
		})
	})

	Describe("when updating the Dash0Monitoring resource", func() {
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
		})

		It("should instrument workloads when the Dash0Monitoring resource is switched from instrumentWorkloads=none to instrumentWorkloads=all", func() { //nolint
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				operatorNamespace,
				dash0MonitoringValues{
					IngressEndpoint:     defaultIngressEndpoint,
					InstrumentWorkloads: dash0v1alpha1.None,
				},
			)

			By("installing the Node.js stateful set")
			Expect(installNodeJsStatefulSet(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js stateful set has not been instrumented by the webhook (due to " +
				"namespace-level opt-out via the Dash0Monitoring resource)")
			Consistently(func(g Gomega) {
				verifyNoDash0Labels(g, applicationUnderTestNamespace, "statefulset")
			}, 10*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

			updateInstrumentWorkloadsModeOfDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0v1alpha1.All,
			)

			By("verifying that the Node.js stateful set has been instrumented by the controller")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"statefulset",
				1210,
				false,
				images,
				"controller",
			)
		})

		It("should revert an instrumented workload when the Dash0Monitoring resource is switched from instrumentWorkloads=all to instrumentWorkloads=none", func() { //nolint
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				operatorNamespace,
				defaultDash0MonitoringValues,
			)

			By("installing the Node.js deployment")
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			testId := verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				images,
				"webhook",
			)

			By("updating the Dash0Monitoring resource to InstrumentWorkloads=none")
			updateInstrumentWorkloadsModeOfDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0v1alpha1.None,
			)

			verifyThatInstrumentationHasBeenReverted(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				testId,
				"controller",
			)
		})

		It("should update the daemonset collector configuration when updating the Dash0 endpoint", func() {
			deployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace, defaultDash0MonitoringValues)

			By("updating the Dash0 monitoring resource ingress endpoint")
			newIngressEndpoint := "ingress.eu-east-1.aws.dash0-dev.com:4317"
			updateIngressEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, newIngressEndpoint)

			By("verify that the config map has been updated by the controller")
			verifyConfigMapContainsString(operatorNamespace, newIngressEndpoint)

			By("verify that the configuration reloader says to have triggered a config change")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"configuration-reloader",
				"Triggering a collector update due to changes to the config files",
			)

			By("verify that the collector appears to have reloaded its configuration")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"opentelemetry-collector",
				"Received signal from OS",
				"Config updated, restart service",
			)
		})
	})

	Describe("operator removal", func() {

		const (
			namespace1 = "e2e-application-under-test-namespace-removal-1"
			namespace2 = "e2e-application-under-test-namespace-removal-2"
		)

		BeforeAll(func() {
			By("creating test namespaces")
			recreateNamespace(namespace1)
			recreateNamespace(namespace2)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(namespace1)
			undeployDash0MonitoringResource(namespace2)
			undeployOperator(operatorNamespace)
		})

		AfterAll(func() {
			By("removing test namespaces")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace1))
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace2))
			undeployOperator(operatorNamespace)
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
				installWorkload: installNodeJsDaemonSet,
			},
			{
				namespace:       "e2e-application-under-test-namespace-removal-2",
				workloadType:    "deployment",
				port:            1207,
				installWorkload: installNodeJsDeployment,
			},
		}

		Describe("when uninstalling the operator via helm", func() {
			It("should remove all Dash0 monitoring resources and uninstrument all workloads", func() {
				By("deploying workloads")
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("deploying the Node.js %s to namespace %s", config.workloadType, config.namespace))
					Expect(config.installWorkload(config.namespace)).To(Succeed())
				})

				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images, true)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					deployDash0MonitoringResource(config.namespace, operatorNamespace, defaultDash0MonitoringValues)
				})

				testIds := make(map[string]string)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller", config.workloadType))
					testIds[config.workloadType] = verifyThatWorkloadHasBeenInstrumented(
						config.namespace,
						config.workloadType,
						config.port,
						false,
						images,
						"controller",
					)
				})

				undeployOperator(operatorNamespace)

				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					verifyThatInstrumentationHasBeenReverted(
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
						verifyDash0MonitoringResourceDoesNotExist(g, config.namespace)
					}
					verifyDash0OperatorReleaseIsNotInstalled(g, operatorNamespace)
				}).Should(Succeed())
				verifyThatCollectorHasBeenRemoved(operatorNamespace)
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
	var err error

	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)

	buildOperatorControllerImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_OPERATOR_CONTROLLER_IMAGE", "")
	if buildOperatorControllerImageFromLocalSourcesRaw != "" {
		buildOperatorControllerImageFromLocalSources, err = strconv.ParseBool(buildOperatorControllerImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
	images.operator.repository = getEnvOrDefault("CONTROLLER_IMG_REPOSITORY", images.operator.repository)
	images.operator.tag = getEnvOrDefault("CONTROLLER_IMG_TAG", images.operator.tag)
	images.operator.digest = getEnvOrDefault("CONTROLLER_IMG_DIGEST", images.operator.digest)
	images.operator.pullPolicy = getEnvOrDefault("CONTROLLER_IMG_PULL_POLICY", images.operator.pullPolicy)

	buildInstrumentationImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_INSTRUMENTATION_IMAGE", "")
	if buildInstrumentationImageFromLocalSourcesRaw != "" {
		buildInstrumentationImageFromLocalSources, err = strconv.ParseBool(buildInstrumentationImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
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

	buildCollectorImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_COLLECTOR_IMAGE", "")
	if buildCollectorImageFromLocalSourcesRaw != "" {
		buildCollectorImageFromLocalSources, err = strconv.ParseBool(buildCollectorImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
	images.collector.repository = getEnvOrDefault(
		"COLLECTOR_IMG_REPOSITORY",
		images.collector.repository,
	)
	images.collector.tag = getEnvOrDefault("COLLECTOR_IMG_TAG", images.collector.tag)
	images.collector.digest = getEnvOrDefault("COLLECTOR_IMG_DIGEST", images.collector.digest)
	images.collector.pullPolicy = getEnvOrDefault(
		"COLLECTOR_IMG_PULL_POLICY",
		images.collector.pullPolicy,
	)

	buildConfigurationReloaderImageFromLocalSourcesRaw := getEnvOrDefault("BUILD_CONFIGURATION_RELOADER_IMAGE", "")
	if buildConfigurationReloaderImageFromLocalSourcesRaw != "" {
		buildConfigurationReloaderImageFromLocalSources, err =
			strconv.ParseBool(buildConfigurationReloaderImageFromLocalSourcesRaw)
		Expect(err).NotTo(HaveOccurred())
	}
	images.configurationReloader.repository = getEnvOrDefault(
		"CONFIGURATION_RELOADER_IMG_REPOSITORY",
		images.configurationReloader.repository,
	)
	images.configurationReloader.tag = getEnvOrDefault("CONFIGURATION_RELOADER_IMG_TAG", images.configurationReloader.tag)
	images.configurationReloader.digest = getEnvOrDefault("CONFIGURATION_RELOADER_IMG_DIGEST",
		images.configurationReloader.digest)
	images.configurationReloader.pullPolicy = getEnvOrDefault(
		"CONFIGURATION_RELOADER_IMG_PULL_POLICY",
		images.configurationReloader.pullPolicy,
	)
}

func getEnvOrDefault(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)
	if isSet {
		return value
	}
	return defaultValue
}
