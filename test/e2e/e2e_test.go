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
	"github.com/joho/godotenv"

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

		Expect(godotenv.Load(dotEnvFile)).To(Succeed())
		e2eKubernetesContext = os.Getenv("E2E_KUBECTX")
		verboseHttp = strings.ToLower(os.Getenv("E2E_VERBOSE_HTTP")) == "true"
		if e2eKubernetesContext == "" {
			Fail(
				fmt.Sprintf(
					"The mandatory setting E2E_KUBECTX is missing in your environment variables (and also in the file %s).",
					dotEnvFile,
				))
		}
		kubeContextHasBeenChanged, originalKubeContext = setKubernetesContext(e2eKubernetesContext)

		// Cleans up the test namespace, otlp sink and the operator. Usually this is cleaned up in AfterAll/AfterEach
		// steps, but for cases where we want to troubleshoot failing e2e tests and have disabled cleanup in After steps
		// we clean up here at the beginning as well.
		cleanupAll()

		checkIfRequiredPortsAreBlocked()
		renderTemplates()

		recreateNamespace(applicationUnderTestNamespace)

		determineContainerImages()
		rebuildAllContainerImages()
		rebuildAppUnderTestContainerImages()

		deployOtlpSink(workingDir)

		setupFinishedSuccessfully = true
	})

	AfterAll(func() {
		uninstallOtlpSink(workingDir)
		removeAllTemporaryManifests()
		undeployNginxIngressController()
		if applicationUnderTestNamespace != "default" {
			By("removing namespace for application under test")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}
		if kubeContextHasBeenChanged {
			revertKubernetesContext(originalKubeContext)
		}
	})

	AfterEach(func() {
		// As an alternative to undeploying all applications under test (deployment, daemonset, cronjob, ...) we
		// could also delete the whole namespace for the application under test after each test case to get rid of
		// all applications (and then recreate the namespace before each test).
		removeAllTestApplications(applicationUnderTestNamespace)
	})

	Describe("controller", func() {
		AfterEach(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployOperator(operatorNamespace)
		})

		controllerTestConfigs := []controllerTestConfig{
			{workloadType: workloadTypeCronjob, runtime: runtimeTypeNodeJs},
			{workloadType: workloadTypeDaemonSet, runtime: runtimeTypeNodeJs},
			{workloadType: workloadTypeDeployment, runtime: runtimeTypeNodeJs},
			{workloadType: workloadTypeDeployment, runtime: runtimeTypeJvm},
			{workloadType: workloadTypeReplicaSet, runtime: runtimeTypeNodeJs},
			{workloadType: workloadTypeStatefulSet, runtime: runtimeTypeNodeJs},
		}

		Describe("when instrumenting existing workloads", func() {
			It("should instrument and uninstrument all workload types", func() {
				testIds := make(map[string]string)
				for _, c := range controllerTestConfigs {
					mapKey := fmt.Sprintf(
						"%s-%s",
						c.runtime.runtimeTypeLabel,
						c.workloadType.workloadTypeString,
					)
					testIds[mapKey] = generateTestId(c.runtime, c.workloadType)
				}

				By("deploying all workloads")
				runInParallel(controllerTestConfigs, func(c controllerTestConfig) {
					By(
						fmt.Sprintf(
							"deploying the %s %s", c.runtime.runtimeTypeLabel, c.workloadType.workloadTypeString))
					Expect(installWorkload(
						c.runtime,
						c.workloadType,
						applicationUnderTestNamespace,
						testIds[fmt.Sprintf(
							"%s-%s",
							c.runtime.runtimeTypeLabel,
							c.workloadType.workloadTypeString,
						)],
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

				// Note: On kind, this fails sometimes due to missing workload resource attribute
				//  - k8s.deployment.name: ! FAILED - expected dash0-operator-nodejs-20-express-test-deployment but the
				//    span has no such attribute
				//  - k8s.pod.name: passed
				//  - timestamp: skipped - no lower bound provided
				//  - span.kind: passed
				//  - http.target: passed
				//  Expected
				//      <bool>: false
				//  to be true
				//  In [It] at: /Users/bastian/dco/test/e2e/verify_instrumentation.go:64 @ 11/26/24 10:28:53.645
				// No amount of retrying helps. Once the collector is in this state, all spans lack that resource
				// attribute. See comment in spans.go#resourceSpansHaveExpectedResourceAttributes.
				runInParallel(controllerTestConfigs, func(c controllerTestConfig) {
					By(fmt.Sprintf("verifying that the %s %s has been instrumented by the controller",
						c.runtime.runtimeTypeLabel,
						c.workloadType.workloadTypeString,
					))
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						c.runtime,
						c.workloadType,
						testIds[fmt.Sprintf(
							"%s-%s",
							c.runtime.runtimeTypeLabel,
							c.workloadType.workloadTypeString,
						)],
						images,
						"controller",
						false,
					)
				})
				By("all workloads have been instrumented")

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				runInParallel(controllerTestConfigs, func(c controllerTestConfig) {
					verifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						c.runtime,
						c.workloadType,
						testIds[fmt.Sprintf(
							"%s-%s",
							c.runtime.runtimeTypeLabel,
							c.workloadType.workloadTypeString,
						)],
						"controller",
					)
				})
				By("all workloads have been reverted")

				verifyThatCollectorHasBeenRemoved(operatorNamespace)
			})
		})

		Describe("when it detects existing jobs or ownerless pods", func() {
			It("should label immutable jobs accordingly", func() {
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeJob)
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
					verifyLabels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypeJob, false, images, "controller")
					verifyFailedInstrumentationEvent(
						g,
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeJob,
						"Dash0 instrumentation of this workload by the controller has not been successful. "+
							"Error message: Dash0 cannot instrument the existing job "+
							"e2e-application-under-test-namespace/dash0-operator-nodejs-20-express-test-job, since "+
							"this type of workload is immutable.",
					)
				}, labelChangeTimeout, pollingInterval).Should(Succeed())

				undeployDash0MonitoringResource(applicationUnderTestNamespace)

				verifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeJob,
				)
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
					verifyNoDash0Labels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypePod)
				}, labelChangeTimeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when updating workloads at startup", func() {
			It("should update instrumentation modifications at startup", func() {
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
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
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					initialAlternativeImages,
					"controller",
					false,
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
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					// check that the new image tags have been applied to the workload
					images,
					"controller",
					false,
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
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)
		})

		AfterAll(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployOperator(operatorNamespace)
		})

		DescribeTable(
			"when instrumenting new workloads",
			func(workloadType workloadType, runtime runtimeType) {
				testId := generateTestId(runtime, workloadType)
				By(fmt.Sprintf("installing the %s %s", runtime.runtimeTypeLabel, workloadType.workloadTypeString))
				Expect(installWorkload(runtime, workloadType, applicationUnderTestNamespace, testId)).To(Succeed())
				By(fmt.Sprintf("verifying that the %s %s has been instrumented by the webhook",
					runtime.runtimeTypeLabel, workloadType.workloadTypeString))
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					runtime,
					workloadType,
					testId,
					images,
					"webhook",
					false,
				)
			},
			Entry("should instrument new Node.js cron jobs", workloadTypeCronjob, runtimeTypeNodeJs),
			Entry("should instrument new JVM daemon sets", workloadTypeDaemonSet, runtimeTypeJvm),
			Entry("should instrument new Node.js daemon sets", workloadTypeDaemonSet, runtimeTypeNodeJs),
			Entry("should instrument new JVM deployments", workloadTypeDeployment, runtimeTypeJvm),
			Entry("should instrument new Node.js deployments", workloadTypeDeployment, runtimeTypeNodeJs),
			Entry("should instrument new Node.js jobs", workloadTypeJob, runtimeTypeNodeJs),
			Entry("should instrument new JVM pods", workloadTypePod, runtimeTypeJvm),
			Entry("should instrument new Node.js pods", workloadTypePod, runtimeTypeNodeJs),
			Entry("should instrument new JVM replica sets", workloadTypeReplicaSet, runtimeTypeJvm),
			Entry("should instrument new Node.js replica sets", workloadTypeReplicaSet, runtimeTypeNodeJs),
			Entry("should instrument new JVM stateful sets", workloadTypeStatefulSet, runtimeTypeJvm),
			Entry("should instrument new Node.js stateful sets", workloadTypeStatefulSet, runtimeTypeNodeJs),
		)

		It("should revert an instrumented workload when the opt-out label is added after the fact", func() {
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
			By("installing the Node.js deployment")
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js deployment has been instrumented by the webhook")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				runtimeTypeNodeJs,
				workloadTypeDeployment,
				testId,
				images,
				"webhook",
				false,
			)

			By("adding the opt-out label to the deployment")
			Expect(addOptOutLabel(
				applicationUnderTestNamespace,
				"deployment",
				"dash0-operator-nodejs-20-express-test-deployment",
			)).To(Succeed())

			verifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
				applicationUnderTestNamespace,
				runtimeTypeNodeJs,
				workloadTypeDeployment,
				testId,
				"webhook",
			)
		})

		It("should instrument a workload when the opt-out label is removed from it", func() {
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeDaemonSet)
			By("installing the Node.js daemonset with dash0.com/enable=false")
			Expect(installNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
			By("verifying that the Node.js daemonset has not been instrumented by the webhook")
			Consistently(func(g Gomega) {
				verifyNoDash0LabelsOrOnlyOptOut(
					g,
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDaemonSet,
					true,
				)
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
				runtimeTypeNodeJs,
				workloadTypeDaemonSet,
				testId,
				images,
				"webhook",
				false,
			)
		})
	})

	Describe("when attempting to revert the instrumentation for jobs", func() {
		// For all other workload types, reverting the instrumentation is tested by the controller test
		// suite. But the controller cannot instrument jobs, so we cannot test the (failing)
		// uninstrumentation procedure there. Thus, for jobs, we test the failing uninstrumentation and
		// its effects here separately.

		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
			)
			deployDash0MonitoringResource(
				applicationUnderTestNamespace,
				defaultDash0MonitoringValues,
				operatorNamespace,
				operatorHelmChart,
			)
		})

		AfterAll(func() {
			undeployDash0MonitoringResource(applicationUnderTestNamespace)
			undeployOperator(operatorNamespace)
		})

		It("when instrumenting a job via webhook and then trying to uninstrument it via the controller", func() {
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeJob)
			By(fmt.Sprintf("installing the %s %s", runtimeTypeNodeJs.runtimeTypeLabel, workloadTypeJob.workloadTypeString))
			Expect(installWorkload(runtimeTypeNodeJs, workloadTypeJob, applicationUnderTestNamespace, testId)).To(Succeed())
			By(fmt.Sprintf("verifying that the %s %s has been instrumented by the webhook",
				runtimeTypeNodeJs.runtimeTypeLabel, workloadTypeJob.workloadTypeString))
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				runtimeTypeNodeJs,
				workloadTypeJob,
				testId,
				images,
				"webhook",
				false,
			)

			By("verifying that removing the Dash0 monitoring resource attempts to uninstruments the job")
			undeployDash0MonitoringResource(applicationUnderTestNamespace)

			Eventually(func(g Gomega) {
				// Verify that the instrumentation labels are still in place -- since we cannot undo the
				// instrumentation, the labels must also not be removed.
				By("verifying that the job still has labels")
				verifyLabels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypeJob, true, images, "webhook")

				By("verifying failed uninstrumentation event")
				verifyFailedUninstrumentationEvent(
					g,
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeJob,
					fmt.Sprintf("The controller's attempt to remove the Dash0 instrumentation from this "+
						"workload has not been successful. Error message: Dash0 cannot remove the "+
						"instrumentation from the existing job e2e-application-under-test-namespace/%s-job, "+
						"since this type of workload is immutable.", runtimeTypeNodeJs.workloadName),
				)
			}, labelChangeTimeout, pollingInterval).Should(Succeed())
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
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeStatefulSet)
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
				verifyNoDash0Labels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypeStatefulSet)
			}, 10*time.Second, pollingInterval).Should(Succeed())

			updateInstrumentWorkloadsModeOfDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0v1alpha1.All,
			)

			By("verifying that the Node.js stateful set has been instrumented by the controller")
			verifyThatWorkloadHasBeenInstrumented(
				applicationUnderTestNamespace,
				runtimeTypeNodeJs,
				workloadTypeStatefulSet,
				testId,
				images,
				"controller",
				false,
			)
		})

		It("should revert an instrumented workload when the Dash0Monitoring resource is switched from instrumentWorkloads=all to instrumentWorkloads=none", func() { //nolint
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
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
				runtimeTypeNodeJs,
				workloadTypeDeployment,
				testId,
				images,
				"webhook",
				false,
			)

			By("updating the Dash0Monitoring resource to InstrumentWorkloads=none")
			updateInstrumentWorkloadsModeOfDash0MonitoringResource(
				applicationUnderTestNamespace,
				dash0v1alpha1.None,
			)

			verifyThatInstrumentationHasBeenReverted(
				applicationUnderTestNamespace,
				runtimeTypeNodeJs,
				workloadTypeDeployment,
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

			It("should instrument workloads", func() {
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
				By("installing the Node.js deployment")
				Expect(installNodeJsWorkload(workloadTypeDeployment, applicationUnderTestNamespace, testId)).To(Succeed())
				By("verifying that the Node.js deployment has been instrumented by the webhook")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					images,
					"webhook",
					true,
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
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
				By("installing the Node.js deployment")
				Expect(installNodeJsWorkload(workloadTypeDeployment, applicationUnderTestNamespace, testId)).To(Succeed())
				By("verifying that the Node.js deployment has been instrumented by the webhook")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					images,
					"webhook",
					true,
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
			testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
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
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					true,
					images,
					"webhook",
				)
			}).Should(Succeed())

			By("waiting for the application under test to become ready")
			Eventually(func(g Gomega) {
				// Make sure the application under test is up and can serve requests, before sending the actual request
				// to trigger the unique log message is sent.
				sendReadyProbe(g, runtimeTypeNodeJs, workloadTypeDeployment)
			}, 30*time.Second, 300*time.Millisecond).Should(Succeed())

			By("sending a request to the Node.js deployment that will generate a log with a predictable body")
			timestampLowerBound := time.Now()
			sendRequest(
				Default,
				runtimeTypeNodeJs,
				workloadTypeDeployment,
				"/dash0-k8s-operator-test",
				fmt.Sprintf("id=%s", testId),
			)

			By("waiting for the the log to appear")

			Eventually(func(g Gomega) error {
				return verifyExactlyOneLogRecordIsReported(g, testId, timestampLowerBound)
			}, 15*time.Second, pollingInterval).Should(Succeed())

			By("churning collector pods")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "pods", "-n", operatorNamespace))

			waitForCollectorToStart(operatorNamespace, operatorHelmChart)

			By("verifying that the previous log message is not reported again (checking for 30 seconds)")
			Consistently(func(g Gomega) error {
				return verifyExactlyOneLogRecordIsReported(g, testId, timestampLowerBound)
			}, 30*time.Second, pollingInterval).Should(Succeed())
		})
	})

	Describe("metrics & self-monitoring", func() {
		var timestampLowerBound time.Time
		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)

			deployDash0OperatorConfigurationResource(defaultDash0OperatorConfigurationValues)
			timestampLowerBound = time.Now()
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
				verifyKubeletStatsMetrics(g, timestampLowerBound)
			}, 50*time.Second, time.Second).Should(Succeed())
		})

		It("should produce cluster metrics via the k8s_cluster receiver", func() {
			By("waiting for k8s_cluster receiver metrics")
			Eventually(func(g Gomega) {
				verifyK8skClusterReceiverMetrics(g, timestampLowerBound)
			}, 50*time.Second, time.Second).Should(Succeed())
		})

		It("should produce Prometheus metrics via the prometheus receiver", func() {
			By("waiting for prometheus receiver metrics")
			Eventually(func(g Gomega) {
				verifyPrometheusMetrics(g, timestampLowerBound)
			}, 90*time.Second, time.Second).Should(Succeed())
		})

		It("should produce self-monitoring telemetry", func() {
			// Note: changing the endpoint will trigger a metric data point for
			// dash0.operator.manager.monitoring.reconcile_requests to be produced.
			// Since we do not mess with the endpoint for the operator configuration resource, the self-monitoring
			// telemetry still ends up in otlp-sink.
			By("updating the Dash0 monitoring resource endpoint setting")
			newEndpoint := "ingress.us-east-2.aws.dash0-dev.com:4317"
			updateEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, newEndpoint)
			defer func() {
				// reset the endpoint to the default after this test
				updateEndpointOfDash0MonitoringResource(applicationUnderTestNamespace, defaultEndpoint)
			}()

			By("waiting for self-monitoring metrics")
			Eventually(func(g Gomega) {
				verifySelfMonitoringMetrics(g, timestampLowerBound)
			}, 90*time.Second, time.Second).Should(Succeed())
		})
	})

	Describe("collect basic metrics without having a Dash0 monitoring resource ", func() {
		var timestampLowerBound time.Time

		BeforeAll(func() {
			By("deploy the Dash0 operator")
			deployOperatorWithDefaultAutoOperationConfiguration(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
			)
			timestampLowerBound = time.Now()
			time.Sleep(5 * time.Second)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		BeforeEach(func() {
			Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
		})

		It("should collect metrics without deploying a Dash0 monitoring resource", func() {
			By("waiting for metrics")
			Eventually(func(g Gomega) {
				verifyNonNamespaceScopedKubeletStatsMetricsOnly(g, timestampLowerBound)
			}, 50*time.Second, time.Second).Should(Succeed())
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
				runtime:      runtimeTypeNodeJs,
			},
			{
				namespace:    "e2e-application-under-test-namespace-removal-2",
				workloadType: workloadTypeDeployment,
				runtime:      runtimeTypeNodeJs,
			},
		}

		Describe("when uninstalling the operator via helm", func() {
			It("should remove all Dash0 monitoring resources and uninstrument all workloads", func() {
				By("deploying workloads")
				testIds := make(map[string]string)
				for _, config := range configs {
					testIds[config.workloadType.workloadTypeString] =
						generateTestId(config.runtime, config.workloadType)
				}

				runInParallel(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("deploying the %s %s to namespace %s",
						config.runtime.runtimeTypeLabel,
						config.workloadType.workloadTypeString,
						config.namespace,
					))
					Expect(installWorkload(
						config.runtime,
						config.workloadType,
						config.namespace,
						testIds[config.workloadType.workloadTypeString],
					)).To(Succeed())
				})

				deployOperator(operatorNamespace, operatorHelmChart, operatorHelmChartUrl, images)
				runInParallel(configs, func(config removalTestNamespaceConfig) {
					deployDash0MonitoringResource(
						config.namespace,
						defaultDash0MonitoringValues,
						operatorNamespace,
						operatorHelmChart,
					)
				})

				runInParallel(configs, func(config removalTestNamespaceConfig) {
					By(fmt.Sprintf("verifying that the %s %s has been instrumented by the controller",
						config.runtime.runtimeTypeLabel,
						config.workloadType.workloadTypeString,
					))
					verifyThatWorkloadHasBeenInstrumented(
						config.namespace,
						config.runtime,
						config.workloadType,
						testIds[config.workloadType.workloadTypeString],
						images,
						"controller",
						false,
					)
				})

				undeployOperator(operatorNamespace)

				runInParallel(configs, func(config removalTestNamespaceConfig) {
					verifyThatInstrumentationHasBeenReverted(
						config.namespace,
						config.runtime,
						config.workloadType,
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

type controllerTestConfig struct {
	workloadType workloadType
	runtime      runtimeType
}

func (c controllerTestConfig) GetWorkloadType() workloadType {
	return c.workloadType
}

func (c controllerTestConfig) GetRuntimeType() runtimeType {
	return c.runtime
}

type removalTestNamespaceConfig struct {
	namespace    string
	workloadType workloadType
	runtime      runtimeType
}

func (c removalTestNamespaceConfig) GetWorkloadType() workloadType {
	return c.workloadType
}

func (c removalTestNamespaceConfig) GetRuntimeType() runtimeType {
	return c.runtime
}

func cleanupAll() {
	if applicationUnderTestNamespace != "default" {
		By("removing namespace for application under test")
		_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace, "--ignore-not-found"))
	}
	undeployOperator(operatorNamespace)
	uninstallOtlpSink(workingDir)
}

func determineContainerImages() {
	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)

	// The defaults when using the Helm chart from local sources are:
	// - use the locally built image with tag latest
	// - use pull policy "Never"
	// The defaults when using a remote helm chart are:
	// - default to not setting an explicit image repository or tag
	// - default to not setting a pull policy, instead let Kubernetes use the default pull policy
	if !isLocalHelmChart() {
		images = emptyImages
	}

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

	if isLocalHelmChart() {
		// support using the local helm chart with remote images
		if isRemoteImage(images.operator) {
			images.operator.pullPolicy = getEnvOrDefault("CONTROLLER_IMG_PULL_POLICY", "")
		}
		if isRemoteImage(images.instrumentation) {
			images.instrumentation.pullPolicy = getEnvOrDefault("INSTRUMENTATION_IMG_PULL_POLICY", "")
		}
		if isRemoteImage(images.collector) {
			images.collector.pullPolicy = getEnvOrDefault("COLLECTOR_IMG_PULL_POLICY", "")
		}
		if isRemoteImage(images.configurationReloader) {
			images.configurationReloader.pullPolicy = getEnvOrDefault("CONFIGURATION_RELOADER_IMG_PULL_POLICY", "")
		}
		if isRemoteImage(images.fileLogOffsetSynch) {
			images.fileLogOffsetSynch.pullPolicy = getEnvOrDefault("FILELOG_OFFSET_SYNCH_IMG_PULL_POLICY", "")
		}
	}
}

func getEnvOrDefault(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)
	if isSet {
		return value
	}
	return defaultValue
}

func generateTestId(runtime runtimeType, workloadType workloadType) string {
	testIdUuid := uuid.New()
	testId := testIdUuid.String()
	By(fmt.Sprintf("%s %s: test ID: %s", runtime.runtimeTypeLabel, workloadType.workloadTypeString, testId))
	return testId
}
