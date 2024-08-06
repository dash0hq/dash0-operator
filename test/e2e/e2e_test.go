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
	"github.com/onsi/gomega/format"
)

const (
	kubeContextForTest            = "minikube"
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"
)

var (
	workingDir string

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
		instrumentation: ImageSpec{
			repository: "instrumentation",
			tag:        "latest",
			pullPolicy: "Never",
		},
	}
)

var _ = Describe("Dash0 Kubernetes Operator", Ordered, func() {

	BeforeAll(func() {
		// Do not truncate string diff output
		format.MaxLength = 0

		pwdOutput, err := Run(exec.Command("pwd"), false)
		Expect(err).NotTo(HaveOccurred())
		workingDir = strings.TrimSpace(pwdOutput)
		fmt.Fprintf(GinkgoWriter, "workingDir: %s\n", workingDir)

		kubeContextHasBeenChanged, originalKubeContext = SetKubeContext(kubeContextForTest)
		CheckIfRequiredPortsAreBlocked()
		RenderTemplates()

		certManagerHasBeenInstalled = EnsureCertManagerIsInstalled()
		RecreateNamespace(applicationUnderTestNamespace)

		readAndApplyEnvironmentVariables()
		RebuildOperatorControllerImage(images.operator, buildOperatorControllerImageFromLocalSources)
		RebuildOperatorConfigurationReloaderImage(images.configurationReloader, buildOperatorControllerImageFromLocalSources)
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
			// could also delete the whole namespace for the application under test after each test case to get rid of
			// all applications (and then recreate the namespace before each test).
			RemoveAllTestApplications(applicationUnderTestNamespace)
			DeleteTestIdFiles()
		}
	})

	Describe("controller", func() {
		AfterEach(func() {
			UndeployDash0MonitoringResource(applicationUnderTestNamespace)
			UndeployOperator(operatorNamespace)
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

				DeployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					true,
					workingDir,
				)
				DeployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace)

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

				UndeployDash0MonitoringResource(applicationUnderTestNamespace)

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

				VerifyThatOTelCollectorHasBeenRemoved(operatorNamespace)
			})
		})

		Describe("when it detects existing jobs or ownerless pods", func() {
			It("should label immutable jobs accordingly", func() {
				By("installing the Node.js job")
				Expect(InstallNodeJsJob(applicationUnderTestNamespace)).To(Succeed())
				DeployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					true,
					workingDir,
				)
				DeployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace)
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

				UndeployDash0MonitoringResource(applicationUnderTestNamespace)

				VerifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(applicationUnderTestNamespace, "job")
			})

			It("should ignore existing pods", func() {
				By("installing the Node.js pod")
				Expect(InstallNodeJsPod(applicationUnderTestNamespace)).To(Succeed())
				DeployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					true,
					workingDir,
				)
				DeployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace)
				By("verifying that the Node.js pod has not been labelled")
				Eventually(func(g Gomega) {
					VerifyNoDash0Labels(g, applicationUnderTestNamespace, "pod")
				}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
			})
		})

		Describe("when updating workloads at startup", func() {
			It("should update instrumentation modifications at startup", func() {
				By("installing the Node.js deployment")
				Expect(InstallNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

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
				}
				DeployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					// we deploy the chart with image digests initially, so that the helm upgrade command we run later
					// (with image tags instead of digests) will actually change the reference to the image (even
					// if it is the same image content).
					initialImages,
					false,
					workingDir,
				)
				DeployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace)

				By("verifying that the Node.js deployment has been instrumented by the controller")
				VerifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					1207,
					false,
					initialImages,
					"controller",
				)

				UpgradeOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					// now we use different image tags
					images,
					false,
				)

				By("verifying that the Node.js deployment's instrumentation settings have been updated by the controller")
				VerifyThatWorkloadHasBeenInstrumented(
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
			DeployOperator(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
				true,
				workingDir,
			)

			fmt.Fprint(GinkgoWriter, "waiting 10 seconds to give the webhook some time to get ready\n")
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			UndeployOperator(operatorNamespace)
		})

		BeforeEach(func() {
			DeployDash0MonitoringResource(applicationUnderTestNamespace, operatorNamespace)
		})

		AfterEach(func() {
			UndeployDash0MonitoringResource(applicationUnderTestNamespace)
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
					By("verifying that removing the Dash0 monitoring resource attempts to uninstruments the job")
					UndeployDash0MonitoringResource(applicationUnderTestNamespace)

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

		It("should revert an instrumented workload when the opt-out label is added after the fact", func() {
			By("installing the Node.js deployment")
			Expect(InstallNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			testId := VerifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				images,
				"webhook",
			)

			By("Adding the opt-out label to the deployment")
			Expect(AddOptOutLabel(
				applicationUnderTestNamespace,
				"deployment",
				"dash0-operator-nodejs-20-express-test-deployment",
			)).To(Succeed())

			VerifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
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
			Expect(InstallNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js daemonset has not been instrumented by the webhook")
			Consistently(func(g Gomega) {
				verifyNoDash0Labels(g, applicationUnderTestNamespace, "daemonset", true)
			}, 10*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

			By("Removing the opt-out label from the daemonset")
			Expect(RemoveOptOutLabel(
				applicationUnderTestNamespace,
				"daemonset",
				"dash0-operator-nodejs-20-express-test-daemonset",
			)).To(Succeed())

			By("verifying that the Node.js daemonset has been instrumented by the webhook")
			VerifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"daemonset",
				1206,
				false,
				images,
				"webhook",
			)
		})

		FIt("should update the daemonset collector configuration when updating the Dash0 endpoint", func() {
			By("updating the Dash0 resource endpoint")

			newEndpoint := "ingress.eu-east-1.aws.dash0-dev.com:4317"
			Expect(
				RunAndIgnoreOutput(exec.Command(
					"kubectl",
					"patch",
					"-n",
					applicationUnderTestNamespace,
					"Dash0", // TODO Update to Dash0Monitoring after rebase
					dash0CustomResourceName,
					"--type",
					"merge",
					"-p",
					"{\"spec\":{\"ingressEndpoint\":\""+newEndpoint+"\"}}",
				))).To(Succeed())

			resourceName := fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName)

			Eventually(func(g Gomega) {
				// Check that the collector appears to have reloaded configuration
				output, err := Run(exec.Command(
					"kubectl",
					"get",
					"-n",
					operatorNamespace,
					"configmap/"+resourceName,
					"-o",
					"json",
				))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(output).To(ContainSubstring(newEndpoint))
			}, 10*time.Second, time.Second).Should(Succeed())

			// Wait up to 10 seconds for the changes to apply to all daemonsets
			Eventually(func(g Gomega) {
				// Check that the configreloader says to have triggered a config change
				output, err := Run(exec.Command(
					"kubectl",
					"logs",
					"-n",
					operatorNamespace,
					"daemonset/"+resourceName,
					"-c",
					"configuration-reloader",
				))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Triggering a collector update due to changes to the config files"))

				// Check that the collector appears to have reloaded configuration
				output, err = Run(exec.Command(
					"kubectl",
					"logs",
					"-n",
					operatorNamespace,
					"daemonset/"+resourceName,
					"-c",
					"opentelemetry-collector",
				))
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Received signal from OS"))
				g.Expect(output).To(ContainSubstring("Config updated, restart service"))
			}, 10*time.Second, time.Second).Should(Succeed())
		})

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
			UndeployDash0MonitoringResource(namespace1)
			UndeployDash0MonitoringResource(namespace2)
			UndeployOperator(operatorNamespace)
		})

		AfterAll(func() {
			By("removing test namespaces")
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace1))
			_ = RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace2))
			UndeployOperator(operatorNamespace)
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
			It("should remove all Dash0 monitoring resources and uninstrument all workloads", func() {
				By("deploying workloads")
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("deploying the Node.js %s to namespace %s", config.workloadType, config.namespace))
					Expect(config.installWorkload(config.namespace)).To(Succeed())
				})

				DeployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					true,
					workingDir,
				)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					DeployDash0MonitoringResource(config.namespace, operatorNamespace)
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

				UndeployOperator(operatorNamespace)

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
						VerifyDash0MonitoringResourceDoesNotExist(g, config.namespace)
					}
					VerifyDash0OperatorReleaseIsNotInstalled(g, operatorNamespace)
				}).Should(Succeed())
				VerifyThatOTelCollectorHasBeenRemoved(operatorNamespace)
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
