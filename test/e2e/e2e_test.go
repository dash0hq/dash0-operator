// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/startup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	"github.com/dash0hq/dash0-operator/test/util"
)

const (
	dotEnvFile = "test-resources/.env"
)

var (
	workingDir string
)

var _ = Describe("Dash0 Operator", Ordered, func() {

	BeforeAll(func() {
		// Do not truncate string diff output.
		format.MaxLength = 0

		pwdOutput, err := run(exec.Command("pwd"), false)
		Expect(err).NotTo(HaveOccurred())
		workingDir = strings.TrimSpace(pwdOutput)
		e2ePrint("workingDir: %s\n", workingDir)

		env, err := readDotEnvFile(dotEnvFile)
		Expect(err).NotTo(HaveOccurred())

		e2eKubeCtx := env["E2E_KUBECTX"]
		if e2eKubeCtx == "" {
			Fail(fmt.Sprintf("The mandatory setting E2E_KUBECTX is missing in the file %s.", dotEnvFile))
		}
		kubeContextHasBeenChanged, originalKubeContext = setKubeContext(e2eKubeCtx)

		// Cleans up the test namespace, otlp sink and the operator. Usually this is cleaned up in AfterAll/AfterEach
		// steps, but for cases where we want to troubleshoot failing e2e tests and have disabled cleanup in After steps
		// we clean up here at the beginning as well.
		cleanupAll()

		checkIfRequiredPortsAreBlocked()
		renderTemplates()

		recreateNamespace(applicationUnderTestNamespace)

		readAndApplyEnvironmentVariables()
		rebuildAllContainerImages()
		rebuildNodeJsApplicationContainerImage()

		setupFinishedSuccessfully = true
	})

	AfterAll(func() {
		removeAllTemporaryManifests()
		if applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}
		if kubeContextHasBeenChanged {
			revertKubeCtx(originalKubeContext)
		}
	})

	BeforeEach(func() {
		deployOtlpSink(workingDir)
	})

	AfterEach(func() {
		if setupFinishedSuccessfully {
			// As an alternative to undeploying all applications under test (deployment, daemonset, cronjob, ...) we
			// could also delete the whole namespace for the application under test after each test case to get rid of
			// all applications (and then recreate the namespace before each test).
			removeAllTestApplications(applicationUnderTestNamespace)
			uninstallOtlpSink(workingDir)
		}
	})

	Describe("controller", func() {
		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployOperator(operatorNamespace)
		})

		controllerTestWorkloadTypes := []workloadType{
			workloadTypeCronjob,
			workloadTypeDaemonSet,
			workloadTypeDeployment,
			workloadTypeReplicaSet,
			workloadTypeStatefulSet,
		}

		Describe("when instrumenting existing workloads", func() {
			It("should instrument and uninstrument all workload types", func() {
				testIds := make(map[string]string)
				for _, workloadType := range controllerTestWorkloadTypes {
					testIds[workloadType.workloadTypeString] = generateTestId(workloadType.workloadTypeString)
				}

				By("deploying all workloads")
				runInParallelForAllWorkloadTypes(controllerTestWorkloadTypes, func(workloadType workloadType) {
					By(fmt.Sprintf("deploying the Node.js %s", workloadType.workloadTypeString))
					Expect(installNodeJsWorkload(
						workloadType,
						applicationUnderTestNamespace,
						testIds[workloadType.workloadTypeString],
					)).To(Succeed())
				})
				By("all workloads have been deployed")

				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					defaultDash0MonitoringValues,
					operatorNamespace,
					operatorHelmChart,
				)

				runInParallelForAllWorkloadTypes(controllerTestWorkloadTypes, func(workloadType workloadType) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller",
						workloadType.workloadTypeString))
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						workloadType.workloadTypeString,
						workloadType.port,
						workloadType.isBatch,
						testIds[workloadType.workloadTypeString],
						images,
						"controller",
					)
				})
				By("all workloads have been instrumented")

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				runInParallelForAllWorkloadTypes(controllerTestWorkloadTypes, func(workloadType workloadType) {
					verifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						workloadType.workloadTypeString,
						workloadType.port,
						workloadType.isBatch,
						testIds[workloadType.workloadTypeString],
						"controller",
					)
				})
				By("all workloads have been reverted")

				verifyThatCollectorHasBeenRemoved(operatorNamespace)
			})
		})

		Describe("when it detects existing jobs or ownerless pods", func() {
			It("should label immutable jobs accordingly", func() {
				testId := generateTestId("job")
				By("installing the Node.js job")
				Expect(installNodeJsJob(applicationUnderTestNamespace, testId)).To(Succeed())
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					defaultDash0MonitoringValues,
					operatorNamespace,
					operatorHelmChart,
				)
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
				}, labelChangeTimeout, pollingInterval).Should(Succeed())

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				verifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(applicationUnderTestNamespace, "job")
			})

			It("should ignore existing pods", func() {
				By("installing the Node.js pod")
				Expect(installNodeJsPod(applicationUnderTestNamespace)).To(Succeed())
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					defaultDash0MonitoringValues,
					operatorNamespace,
					operatorHelmChart,
				)
				By("verifying that the Node.js pod has not been labelled")
				Eventually(func(g Gomega) {
					verifyNoDash0Labels(g, applicationUnderTestNamespace, "pod")
				}, labelChangeTimeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when updating workloads at startup", func() {
			It("should update instrumentation modifications at startup", func() {
				testId := generateTestId("deployment")
				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				// We initially deploy the operator with alternative image names to simulate the workloads having
				// been instrumented by outdated images. Then (later) we will redeploy the operator with the actual
				// image names that are used throughout the whole test suite (defined by environment variables).

				initialAlternativeImages := Images{
					operator:              deriveAlternativeImageForUpdateTest(images.operator),
					instrumentation:       deriveAlternativeImageForUpdateTest(images.instrumentation),
					collector:             deriveAlternativeImageForUpdateTest(images.collector),
					configurationReloader: deriveAlternativeImageForUpdateTest(images.configurationReloader),
					fileLogOffsetSynch:    deriveAlternativeImageForUpdateTest(images.fileLogOffsetSynch),
				}
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, initialAlternativeImages)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					defaultDash0MonitoringValues,
					operatorNamespace,
					operatorHelmChart,
				)

				By("verifying that the Node.js deployment has been instrumented by the controller")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					1207,
					false,
					testId,
					initialAlternativeImages,
					"controller",
				)

				// Now update the operator with the actual image names that are used throughout the whole test suite.
				upgradeOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					// now we use :latest (or :main-dev or whatever has been provided via env vars) instead of
					// :e2e-test to trigger an actual change
					images,
				)

				By("verifying that the Node.js deployment's instrumentation settings have been updated by the controller")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					1207,
					false,
					testId,
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
			deployOperator(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
			)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		BeforeEach(func() {
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
		})

		DescribeTable(
			"when instrumenting new workloads",
			func(workloadType workloadType) {
				testId := generateTestId(workloadType.workloadTypeString)
				By(fmt.Sprintf("installing the Node.js %s", workloadType.workloadTypeString))
				Expect(installNodeJsWorkload(workloadType, applicationUnderTestNamespace, testId)).To(Succeed())
				By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the webhook",
					workloadType.workloadTypeString))
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					workloadType.workloadTypeString,
					workloadType.port,
					workloadType.isBatch,
					testId,
					images,
					"webhook",
				)

				if workloadType.workloadTypeString == "job" {
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
						verifyLabels(g, applicationUnderTestNamespace, workloadType.workloadTypeString, true, images, "webhook")

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
					}, labelChangeTimeout, pollingInterval).Should(Succeed())
				}
			},
			Entry("should instrument new cron jobs", workloadTypeCronjob),
			Entry("should instrument new daemon sets", workloadTypeDaemonSet),
			Entry("should instrument new deployments", workloadTypeDeployment),
			Entry("should instrument new jobs", workloadTypeJob),
			Entry("should instrument new pods", workloadTypePod),
			Entry("should instrument new replica sets", workloadTypeReplicaSet),
			Entry("should instrument new stateful sets", workloadTypeStatefulSet),
		)

		It("should revert an instrumented workload when the opt-out label is added after the fact", func() {
			testId := generateTestId("deployment")
			By("installing the Node.js deployment")
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				testId,
				images,
				"webhook",
			)

			By("adding the opt-out label to the deployment")
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
			testId := generateTestId("daemonset")
			By("installing the Node.js daemonset with dash0.com/enable=false")
			Expect(installNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js daemonset has not been instrumented by the webhook")
			Consistently(func(g Gomega) {
				verifyNoDash0LabelsOrOnlyOptOut(g, applicationUnderTestNamespace, "daemonset", true)
			}, 10*time.Second, pollingInterval).Should(Succeed())

			By("removing the opt-out label from the daemonset")
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
				testId,
				images,
				"webhook",
			)
		})
	})

	Describe("when updating the Dash0OperatorConfiguration resource", func() {
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployDash0OperatorConfigurationResource()
		})

		//nolint:lll
		It("should update the daemonset collector configuration when updating the Dash0 endpoint in the operator configuration resource", func() {
			deployDash0OperatorConfigurationResource(dash0OperatorConfigurationValues{
				SelfMonitoringEnabled: false,
				Endpoint:              defaultEndpoint,
				Token:                 defaultToken,
			})
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0MonitoringValues{
					Endpoint:            "",
					Token:               "",
					InstrumentWorkloads: dash0v1alpha1.All,
				},
				operatorNamespace,
				operatorHelmChart,
			)

			time.Sleep(10 * time.Second)

			By("updating the Dash0 operator configuration endpoint setting")
			newEndpoint := "ingress.eu-east-1.aws.dash0-dev.com:4317"
			updateEndpointOfDash0OperatorConfigurationResource(newEndpoint)

			By("verify that the config map has been updated by the controller")
			verifyConfigMapContainsString(operatorNamespace, newEndpoint)

			// This step sometimes takes quite a while, for some reason the config map change is not seen immediately
			// from within the collector pod/config-reloader container process, although it polls the file every second.
			// Thus, we allow a very generous timeout here.
			By("verify that the configuration reloader says to have triggered a config change")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"configuration-reloader",
				90*time.Second,
				"Triggering a collector update due to changes to the config files",
			)

			By("verify that the collector appears to have reloaded its configuration")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"opentelemetry-collector",
				20*time.Second,
				"Received signal from OS",
				"Config updated, restart service",
			)
		})
	})

	Describe("when updating the Dash0Monitoring resource", func() {
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
		})

		It("should instrument workloads when the Dash0Monitoring resource is switched from instrumentWorkloads=none to instrumentWorkloads=all", func() { //nolint
			testId := generateTestId("statefulset")
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0MonitoringValues{
					Endpoint:            defaultEndpoint,
					Token:               defaultToken,
					InstrumentWorkloads: dash0v1alpha1.None,
				},
				operatorNamespace,
				operatorHelmChart,
			)

			By("installing the Node.js stateful set")
			Expect(installNodeJsStatefulSet(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js stateful set has not been instrumented by the webhook (due to " +
				"namespace-level opt-out via the Dash0Monitoring resource)")
			Consistently(func(g Gomega) {
				verifyNoDash0Labels(g, applicationUnderTestNamespace, "statefulset")
			}, 10*time.Second, pollingInterval).Should(Succeed())

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
				testId,
				images,
				"controller",
			)
		})

		It("should revert an instrumented workload when the Dash0Monitoring resource is switched from instrumentWorkloads=all to instrumentWorkloads=none", func() { //nolint
			testId := generateTestId("deployment")
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)

			By("installing the Node.js deployment")
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				"deployment",
				1207,
				false,
				testId,
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

		//nolint:lll
		It("should update the daemonset collector configuration when updating the Dash0 endpoint in the monitoring resource", func() {
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)

			time.Sleep(10 * time.Second)

			By("updating the Dash0 monitoring resource endpoint setting")
			newEndpoint := "ingress.eu-east-1.aws.dash0-dev.com:4317"
			updateEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, newEndpoint)

			By("verify that the config map has been updated by the controller")
			verifyConfigMapContainsString(operatorNamespace, newEndpoint)

			// This step sometimes takes quite a while, for some reason the config map change is not seen immediately
			// from within the collector pod/config-reloader container process, although it polls the file every second.
			// Thus, we allow a very generous timeout here.
			By("verify that the configuration reloader says to have triggered a config change")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"configuration-reloader",
				90*time.Second,
				"Triggering a collector update due to changes to the config files",
			)

			By("verify that the collector appears to have reloaded its configuration")
			verifyCollectorContainerLogContainsStrings(
				operatorNamespace,
				"opentelemetry-collector",
				20*time.Second,
				"Received signal from OS",
				"Config updated, restart service",
			)
		})
	})

	Describe("using the operator configuration resource's connection settings", func() {
		Describe("with a manually created operator configuration resource", func() {
			BeforeAll(func() {
				By("deploy the Dash0 operator")
				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
			})

			AfterAll(func() {
				undeployOperator(operatorNamespace)
			})

			BeforeEach(func() {
				deployDash0OperatorConfigurationResource(dash0OperatorConfigurationValues{
					SelfMonitoringEnabled: false,
					Endpoint:              defaultEndpoint,
					Token:                 defaultToken,
				})
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						Endpoint:            "",
						Token:               "",
						InstrumentWorkloads: dash0v1alpha1.All,
					},
					operatorNamespace,
					operatorHelmChart,
				)
			})

			AfterEach(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				undeployDash0OperatorConfigurationResource()
			})

			It("should instrumenting workloads", func() {
				testId := generateTestId("deployment")
				By("installing the Node.js deployment")
				Expect(installNodeJsWorkload(workloadTypeDeployment, applicationUnderTestNamespace, testId)).To(Succeed())
				By("verifying that the Node.js deployment has been instrumented by the webhook")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					workloadTypeDeployment.port,
					false,
					testId,
					images,
					"webhook",
				)
			})
		})

		Describe("using the automatically created operator configuration resource", func() {
			BeforeAll(func() {
				By("deploy the Dash0 operator and let it create an operator configuration resource")
				deployOperatorWithDefaultAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
				)
			})

			AfterAll(func() {
				undeployOperator(operatorNamespace)
			})

			BeforeEach(func() {
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						Endpoint:            "",
						Token:               "",
						InstrumentWorkloads: dash0v1alpha1.All,
					},
					operatorNamespace,
					operatorHelmChart,
				)
			})

			AfterEach(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				undeployDash0OperatorConfigurationResource()
			})

			It("should instrumenting workloads", func() {
				testId := generateTestId("deployment")
				By("installing the Node.js deployment")
				Expect(installNodeJsWorkload(workloadTypeDeployment, applicationUnderTestNamespace, testId)).To(Succeed())
				By("verifying that the Node.js deployment has been instrumented by the webhook")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					"deployment",
					workloadTypeDeployment.port,
					false,
					testId,
					images,
					"webhook",
				)
			})
		})
	})

	Describe("log collection", func() {

		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
			time.Sleep(10 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
		})

		It("does not collect the same logs twice from a file when the collector pod churns", func() {
			testId := generateTestId("deployment")
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)

			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

			By("verifying that the Node.js deployment has been instrumented by the controller")
			Eventually(func(g Gomega) {
				verifyLabels(
					g,
					applicationUnderTestNamespace,
					"deployment",
					true,
					images,
					"webhook",
				)
			}).Should(Succeed())

			By("sending a request to the Node.js deployment that will generate a log with a predictable body")
			now := time.Now()
			sendRequest(Default, 1207, fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId))

			By("waiting for the the log to appear")

			Eventually(func(g Gomega) error {
				return verifyExactlyOneLogRecordIsReported(g, testId, &now)
			}, 15*time.Second, pollingInterval).Should(Succeed())

			By("churning collector pods")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "pods", "-n", operatorNamespace))

			waitForCollectorToStart(operatorNamespace, operatorHelmChart)

			By("verifying that the previous log message is not reported again (checking for 30 seconds)")
			Consistently(func(g Gomega) error {
				return verifyExactlyOneLogRecordIsReported(g, testId, &now)
			}, 30*time.Second, pollingInterval).Should(Succeed())
		})
	})

	Describe("metrics & self-monitoring", func() {
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)

			deployDash0OperatorConfigurationResource(defaultDash0OperatorConfigurationValues)
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)
			time.Sleep(5 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)

			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployDash0OperatorConfigurationResource()
		})

		BeforeEach(func() {
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
		})

		It("should produce node-based metrics via the kubeletstats receiver", func() {
			By("waiting for kubeletstats receiver metrics")
			Eventually(func(g Gomega) {
				verifyKubeletStatsMetrics(g)
			}, 50*time.Second, time.Second).Should(Succeed())
		})

		It("should produce cluster metrics via the k8s_cluster receiver", func() {
			By("waiting for k8s_cluster receiver metrics")
			Eventually(func(g Gomega) {
				verifyK8skClusterReceiverMetrics(g)
			}, 50*time.Second, time.Second).Should(Succeed())
		})

		It("should produce Prometheus metrics via the prometheus receiver", func() {
			By("waiting for prometheus receiver metrics")
			Eventually(func(g Gomega) {
				verifyPrometheusMetrics(g)
			}, 90*time.Second, time.Second).Should(Succeed())
		})

		It("should produce self-monitoring telemetry", func() {
			By("updating the Dash0 monitoring resource endpoint setting")
			newEndpoint := "ingress.us-east-2.aws.dash0-dev.com:4317"
			updateEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, newEndpoint)
			defer func() {
				// reset the endpoint to the default after this test
				updateEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, defaultEndpoint)
			}()

			By("waiting for self-monitoring metrics")
			Eventually(func(g Gomega) {
				verifySelfMonitoringMetrics(g)
			}, 90*time.Second, time.Second).Should(Succeed())
		})
	})

	Describe("operator installation", func() {

		It("should fail if asked to create an operator configuration resource with invalid settings", func() {
			err := deployOperatorWithAutoOperationConfiguration(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
				&startup.OperatorConfigurationValues{
					Endpoint: util.EndpointDash0Test,
					// no token, no secret ref
				},
			)
			Expect(err).To(
				MatchError(
					ContainSubstring("operator.dash0Export.enabled is set to true, but neither " +
						"operator.dash0Export.token nor operator.dash0Export.secretRef.name & " +
						"operator.dash0Export.secretRef.key have been provided.")))
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

		configs := []removalTestNamespaceConfig{
			{
				namespace:    "e2e-application-under-test-namespace-removal-1",
				workloadType: workloadTypeDaemonSet,
			},
			{
				namespace:    "e2e-application-under-test-namespace-removal-2",
				workloadType: workloadTypeDeployment,
			},
		}

		Describe("when uninstalling the operator via helm", func() {
			It("should remove all Dash0 monitoring resources and uninstrument all workloads", func() {
				By("deploying workloads")
				testIds := make(map[string]string)
				for _, config := range configs {
					testIds[config.workloadType.workloadTypeString] = generateTestId(config.workloadType.workloadTypeString)
				}

				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("deploying the Node.js %s to namespace %s",
						config.workloadType.workloadTypeString, config.namespace))
					Expect(installNodeJsWorkload(
						config.workloadType,
						config.namespace,
						testIds[config.workloadType.workloadTypeString],
					)).To(Succeed())
				})

				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					deployDash0MonitoringResource(
						config.namespace,
						defaultDash0MonitoringValues,
						operatorNamespace,
						operatorHelmChart,
					)
				})

				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("verifying that the Node.js %s has been instrumented by the controller",
						config.workloadType.workloadTypeString))
					verifyThatWorkloadHasBeenInstrumented(
						config.namespace,
						config.workloadType.workloadTypeString,
						config.workloadType.port,
						false,
						testIds[config.workloadType.workloadTypeString],
						images,
						"controller",
					)
				})

				undeployOperator(operatorNamespace)

				runInParallelForAllWorkloadTypes(configs, func(config removalTestNamespaceConfig) {
					verifyThatInstrumentationHasBeenReverted(
						config.namespace,
						config.workloadType.workloadTypeString,
						config.workloadType.port,
						false,
						testIds[config.workloadType.workloadTypeString],
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

func (wt workloadType) GetWorkloadType() string {
	return wt.workloadTypeString
}

type removalTestNamespaceConfig struct {
	namespace    string
	workloadType workloadType
}

func (c removalTestNamespaceConfig) GetWorkloadType() string {
	return c.workloadType.workloadTypeString
}

func cleanupAll() {
	if applicationUnderTestNamespace != "default" {
		By("removing namespace for application under test")
		_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace, "--ignore-not-found"))
	}
	undeployOperator(operatorNamespace)
	uninstallOtlpSink(workingDir)
}

func readAndApplyEnvironmentVariables() {
	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)

	images.operator.repository = getEnvOrDefault("CONTROLLER_IMG_REPOSITORY", images.operator.repository)
	images.operator.tag = getEnvOrDefault("CONTROLLER_IMG_TAG", images.operator.tag)
	images.operator.digest = getEnvOrDefault("CONTROLLER_IMG_DIGEST", images.operator.digest)
	images.operator.pullPolicy = getEnvOrDefault("CONTROLLER_IMG_PULL_POLICY", images.operator.pullPolicy)

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

	images.fileLogOffsetSynch.repository = getEnvOrDefault(
		"FILELOG_OFFSET_SYNCH_IMG_REPOSITORY",
		images.fileLogOffsetSynch.repository,
	)
	images.fileLogOffsetSynch.tag = getEnvOrDefault("FILELOG_OFFSET_SYNCH_IMG_TAG", images.fileLogOffsetSynch.tag)
	images.fileLogOffsetSynch.digest = getEnvOrDefault("FILELOG_OFFSET_SYNCH_IMG_DIGEST",
		images.fileLogOffsetSynch.digest)
	images.fileLogOffsetSynch.pullPolicy = getEnvOrDefault(
		"FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY",
		images.fileLogOffsetSynch.pullPolicy,
	)
}

func getEnvOrDefault(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)
	if isSet {
		return value
	}
	return defaultValue
}

func generateTestId(workloadType string) string {
	testIdUuid := uuid.New()
	testId := testIdUuid.String()
	By(fmt.Sprintf("%s: test ID: %s", workloadType, testId))
	return testId
}
