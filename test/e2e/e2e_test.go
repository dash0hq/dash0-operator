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
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/startup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegaformat "github.com/onsi/gomega/format"

	"github.com/dash0hq/dash0-operator/test/util"
)

const (
	dotEnvFile = "test-resources/.env"
)

var (
	workingDir string

	stopPodCrashOrOOMKillDetection chan bool

	cleanupSteps = neccessaryCleanupSteps{}
)

var _ = Describe("Dash0 Operator", Ordered, func() {

	BeforeAll(func() {
		By("running BeforeAll hook of test suite root")
		// Do not truncate string diff output.
		gomegaformat.MaxLength = 0

		pwdOutput, err := run(exec.Command("pwd"), false)
		Expect(err).NotTo(HaveOccurred())
		workingDir = strings.TrimSpace(pwdOutput)
		e2ePrint("workingDir: %s\n", workingDir)

		if _, err := os.Stat(dotEnvFile); err == nil {
			Expect(godotenv.Load(dotEnvFile)).To(Succeed())
		} else {
			e2ePrint("%s not found, assuming required environment variables are set via other means\n", dotEnvFile)
		}
		e2eKubernetesContext = os.Getenv("E2E_KUBECTX")
		verboseHttp = strings.ToLower(os.Getenv("E2E_VERBOSE_HTTP")) == "true"
		if e2eKubernetesContext == "" {
			Fail(
				fmt.Sprintf(
					"The mandatory environment variable E2E_KUBECTX is not set, add it to %s or set it explicitly.",
					dotEnvFile,
				))
		}
		kubeContextHasBeenChanged, originalKubeContext = setKubernetesContext(e2eKubernetesContext)

		// Cleans up the test namespace, otlp sink and the operator. Usually this is cleaned up in AfterAll/AfterEach
		// steps, but for cases where we want to troubleshoot failing e2e tests and have disabled cleanup in After steps
		// we clean up here at the beginning as well.
		cleanupAll()

		checkIfRequiredPortsAreBlocked()

		ensureMetricsServerIsInstalled(&cleanupSteps)

		recreateNamespace(applicationUnderTestNamespace)
		cleanupSteps.removeTestApplicationNamespace = true
		recreateNamespace(dash0ApiMockNamespace)
		cleanupSteps.removeApiMockNamespace = true

		determineContainerImages()
		rebuildAllContainerImages()
		rebuildAppUnderTestContainerImages()
		rebuildDash0ApiMockImage()

		deployOtlpSink(workingDir, &cleanupSteps)
		if isKindCluster() {
			deployIngressController(&cleanupSteps)
		}
		deployThirdPartyCrds(&cleanupSteps)

		stopPodCrashOrOOMKillDetection = failOnPodCrashOrOOMKill(&cleanupSteps)

		// If BeforeAll does not complete successfully, we will not have deployed any test applications. Once BeforeAll
		// has finished we do not keep track of test applications in detail, but just assume that a test might have
		// deployed some of them, so we make sure to remove all of them in the AfterEach cleanup hook.
		cleanupSteps.removeTestApplications = true
		By("finished BeforeAll hook of test suite root")
	})

	AfterAll(func() {
		By("running AfterAll hook of test suite root")
		if cleanupSteps.stopOOMDetection {
			stopPodCrashOrOOMKillDetection <- true
		}
		uninstallOtlpSink(workingDir, &cleanupSteps)
		removeAllTemporaryManifests()
		undeployNginxIngressController(&cleanupSteps)

		if cleanupSteps.removeTestApplicationNamespace {
			By("removing namespace for application under test")
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", applicationUnderTestNamespace))
		}
		if cleanupSteps.removeApiMockNamespace {
			_ = runAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", dash0ApiMockNamespace))
		}
		uninstallMetricsServerIfApplicable(&cleanupSteps)

		removeThirdPartyCrds(&cleanupSteps)

		if kubeContextHasBeenChanged {
			revertKubernetesContext(originalKubeContext)
		}
		By("finished AfterAll hook of test suite root")
	})

	BeforeEach(func() {
		By("running BeforeEach hook of test suite root")
		createDirAndDeleteOldCollectedLogs()
	})

	AfterEach(func() {
		if !cleanupSteps.removeTestApplications {
			return
		}
		By("running AfterEach hook of test suite root")
		removeAllTestApplications(applicationUnderTestNamespace)
	})

	// MAINTENANCE NOTE: The test suites (`Describe`) are not necessarily grouped by topics, but they are grouped by the
	// setup they require. I.e. all tests that work with a pre-existing operator deployment with the same standard
	// configuration are grouped together etc. This helps with the test execution speed.

	Describe("with an existing operator deployment and operation configuration resource", func() {
		var operatorStartupTimeLowerBound time.Time
		BeforeAll(func() {
			operatorStartupTimeLowerBound = time.Now()
			By("deploying the Dash0 operator")
			deployOperatorWithDefaultAutoOperationConfiguration(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
				true,
				nil,
			)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		Describe("with a deployed Dash0 monitoring resource", func() {
			BeforeAll(func() {
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesDefault,
					operatorNamespace,
				)
			})

			AfterAll(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
			})

			Describe("webhook", func() {
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
							true,
						)
					},
					Entry("should instrument new Node.js daemon sets", workloadTypeDaemonSet, runtimeTypeNodeJs),
					Entry("should instrument new JVM daemon sets", workloadTypeDaemonSet, runtimeTypeJvm),
					Entry("should instrument new .NET daemon sets", workloadTypeDaemonSet, runtimeTypeDotnet),
					Entry("should instrument new Node.js deployments", workloadTypeDeployment, runtimeTypeNodeJs),
					Entry("should instrument new JVM deployments", workloadTypeDeployment, runtimeTypeJvm),
					Entry("should instrument new .NET deployments", workloadTypeDeployment, runtimeTypeDotnet),
					Entry("should instrument new Node.js jobs", workloadTypeJob, runtimeTypeNodeJs),
					Entry("should instrument new Node.js pods", workloadTypePod, runtimeTypeNodeJs),
					Entry("should instrument new JVM pods", workloadTypePod, runtimeTypeJvm),
					Entry("should instrument new .NET pods", workloadTypePod, runtimeTypeDotnet),
					Entry("should instrument new Node.js replica sets", workloadTypeReplicaSet, runtimeTypeNodeJs),
					Entry("should instrument new JVM replica sets", workloadTypeReplicaSet, runtimeTypeJvm),
					Entry("should instrument new .NET replica sets", workloadTypeReplicaSet, runtimeTypeDotnet),
					Entry("should instrument new Node.js stateful sets", workloadTypeStatefulSet, runtimeTypeNodeJs),
					Entry("should instrument new JVM stateful sets", workloadTypeStatefulSet, runtimeTypeJvm),
					Entry("should instrument new .NET stateful sets", workloadTypeStatefulSet, runtimeTypeDotnet),
					Entry("should instrument new Node.js cron jobs", workloadTypeCronjob, runtimeTypeNodeJs),
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
						true,
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
					By("installing the Node.js daemon set with dash0.com/enable=false")
					Expect(installNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
					By("verifying that the Node.js daemon set has not been instrumented by the webhook")
					Consistently(func(g Gomega) {
						verifyNoDash0LabelsOrOnlyOptOut(
							g,
							applicationUnderTestNamespace,
							runtimeTypeNodeJs,
							workloadTypeDaemonSet,
							true,
						)
					}, 10*time.Second, pollingInterval).Should(Succeed())

					By("removing the opt-out label from the daemon set")
					Expect(removeOptOutLabel(
						applicationUnderTestNamespace,
						"daemonset",
						"dash0-operator-nodejs-20-express-test-daemonset",
					)).To(Succeed())

					By("verifying that the Node.js daemon set has been instrumented by the webhook")
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeDaemonSet,
						testId,
						images,
						"webhook",
						true,
					)
				})
			})

			Describe("self-monitoring log collection", func() {
				It("has operator manager logs", func() {
					By("checking for the log record from the operator manager start")
					Eventually(func(g Gomega) {
						verifyAtLeastOneLogRecord(
							g,
							selfMonitoringLogsResourceMatcherOperatorManager,
							func(logRecord plog.LogRecord, matchResult *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
								expectedLogBody := "operator manager configuration:"
								logBody := logRecord.Body().AsString()
								if logBody == expectedLogBody {
									matchResult.addPassedAssertion(logBodyKey)
								} else {
									matchResult.addFailedAssertion(
										logBodyKey,
										fmt.Sprintf("expected %s but it was %s", expectedLogBody, logBody),
									)
								}
							},
							operatorStartupTimeLowerBound,
						)
					}, 15*time.Second, pollingInterval).Should(Succeed())
				})

				It("has collector logs", func() {
					By("checking for a log record from the collector")
					Eventually(func(g Gomega) {
						verifyAtLeastOneLogRecord(
							g,
							selfMonitoringLogsResourceMatcherCollector,
							func(logRecord plog.LogRecord, matchResult *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
								logBody := logRecord.Body().AsString()
								if logBody == collectorReadyLogMessage {
									matchResult.addPassedAssertion(logBodyKey)
								} else {
									matchResult.addFailedAssertion(
										logBodyKey,
										fmt.Sprintf("expected %s but it was %s", collectorReadyLogMessage, logBody),
									)
								}
							},
							operatorStartupTimeLowerBound,
						)
					}, 45*time.Second, pollingInterval).Should(Succeed())
				})
			})

			Describe("log collection", func() {
				It("collects logs, but does not collect the same logs twice from a file when the collector pod churns", func() {
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
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
						testEndpoint,
						fmt.Sprintf("id=%s", testId),
					)

					By(
						fmt.Sprintf(
							"waiting for a log message with body \"processing request %s\" to appear after min timestamp %v",
							testId,
							timestampLowerBound,
						))
					Eventually(func(g Gomega) {
						verifyExactlyOneLogRecord(
							g,
							runtimeTypeNodeJs,
							workloadTypeDeployment,
							testId,
							timestampLowerBound,
						)
					}, 1*time.Minute, pollingInterval).Should(Succeed())

					By("churning collector pods")
					Expect(
						runAndIgnoreOutput(
							exec.Command(
								"kubectl",
								"--namespace",
								operatorNamespace,
								"delete",
								"pods",
								"-l",
								"app.kubernetes.io/component=agent-collector",
							))).To(Succeed())

					waitForCollectorToStart(operatorNamespace, operatorHelmChart)

					By("verifying that the previous log message is not reported again (checking for 30 seconds)")
					Consistently(func(g Gomega) {
						verifyExactlyOneLogRecord(
							g,
							runtimeTypeNodeJs,
							workloadTypeDeployment,
							testId,
							timestampLowerBound,
						)
					}, 30*time.Second, pollingInterval).Should(Succeed())
				})
			})

			Describe("synchronizing Dash0 API resources", func() {
				BeforeAll(func() {
					installDash0ApiMock()
				})

				AfterEach(func() {
					cleanupStoredApiRequests()
					removeDash0ApiSyncResources(applicationUnderTestNamespace)
				})

				AfterAll(func() {
					uninstallDash0ApiMock()
				})

				//nolint:dupl
				It("should synchronize a synthetic check to the Dash0 API", func() {
					deploySyntheticCheckResource(
						applicationUnderTestNamespace,
						dash0ApiResourceValues{},
					)

					//nolint:lll
					routeRegex := "/api/synthetic-checks/dash0-operator_.*_default_e2e-application-under-test-namespace_synthetic-check-e2e-test\\?dataset=default"

					By("verifying the synthetic check has been synchronized to the Dash0 API via PUT")
					req := fetchCapturedApiRequest(0)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(req.Body).ToNot(BeNil())
					Expect(*req.Body).To(ContainSubstring("This is a test synthetic check."))

					setOptOutLabelInSyntheticCheck(applicationUnderTestNamespace, "false")
					By("verifying the synthetic check has been deleted via the Dash0 API (after setting dash0.com/enable=false)\"")
					req = fetchCapturedApiRequest(1)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))

					setOptOutLabelInSyntheticCheck(applicationUnderTestNamespace, "true")
					//nolint:lll
					By("verifying the synthetic check has been synchronized to the Dash0 API via PUT (after setting dash0.com/enable=true)")
					req = fetchCapturedApiRequest(2)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(*req.Body).To(ContainSubstring("This is a test synthetic check."))

					removeSyntheticCheckResource(applicationUnderTestNamespace)
					By("verifying the synthetic check has been deleted via the Dash0 API (after removing the resource)")
					req = fetchCapturedApiRequest(3)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
				})

				//nolint:dupl
				It("should synchronize a view to the Dash0 API", func() {
					deployViewResource(
						applicationUnderTestNamespace,
						dash0ApiResourceValues{},
					)

					//nolint:lll
					routeRegex := "/api/views/dash0-operator_.*_default_e2e-application-under-test-namespace_view-e2e-test\\?dataset=default"

					By("verifying the view has been synchronized to the Dash0 API via PUT")
					req := fetchCapturedApiRequest(0)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(req.Body).ToNot(BeNil())
					Expect(*req.Body).To(ContainSubstring("\"name\":\"E2E test view\""))

					setOptOutLabelInView(applicationUnderTestNamespace, "false")
					By("verifying the view has been deleted via the Dash0 API (after setting dash0.com/enable=false)\"")
					req = fetchCapturedApiRequest(1)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))

					setOptOutLabelInView(applicationUnderTestNamespace, "true")
					//nolint:lll
					By("verifying the view has been synchronized to the Dash0 API via PUT (after setting dash0.com/enable=true)")
					req = fetchCapturedApiRequest(2)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(*req.Body).To(ContainSubstring("\"name\":\"E2E test view\""))

					removeViewResource(applicationUnderTestNamespace)
					By("verifying the view has been deleted via the Dash0 API (after removing the resource)")
					req = fetchCapturedApiRequest(3)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
				})

				//nolint:dupl
				It("should synchronize Perses dashboards to the Dash0 API", func() {
					deployPersesDashboardResource(
						applicationUnderTestNamespace,
						dash0ApiResourceValues{},
					)

					//nolint:lll
					routeRegex := "/api/dashboards/dash0-operator_.*_default_e2e-application-under-test-namespace_perses-dashboard-e2e-test\\?dataset=default"

					By("verifying the dashboard has been synchronized to the Dash0 API via PUT")
					req := fetchCapturedApiRequest(0)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(req.Body).ToNot(BeNil())
					Expect(*req.Body).To(ContainSubstring("This is a test dashboard."))

					setOptOutLabelInPersesDashboard(applicationUnderTestNamespace, "false")
					By("verifying the dashboard has been deleted via the Dash0 API (after setting dash0.com/enable=false)\"")
					req = fetchCapturedApiRequest(1)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))

					setOptOutLabelInPersesDashboard(applicationUnderTestNamespace, "true")
					By("verifying the dashboard has been synchronized to the Dash0 API via PUT (after setting dash0.com/enable=true)")
					req = fetchCapturedApiRequest(2)
					Expect(req.Method).To(Equal("PUT"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
					Expect(*req.Body).To(ContainSubstring("This is a test dashboard."))

					removePersesDashboardResource(applicationUnderTestNamespace)
					By("verifying the dashboard has been deleted via the Dash0 API (after removing the resource)")
					req = fetchCapturedApiRequest(3)
					Expect(req.Method).To(Equal("DELETE"))
					Expect(req.Url).To(MatchRegexp(routeRegex))
				})

				//nolint:lll
				It("should synchronize Prometheus rules to the Dash0 API", func() {
					deployPrometheusRuleResource(
						applicationUnderTestNamespace,
						dash0ApiResourceValues{},
					)

					routeRegexes := []string{
						"/api/alerting/check-rules\\?dataset=default&idPrefix=dash0-operator_.*_default_e2e-application-under-test-namespace_prometheus-rules-e2e-test_",
						"/api/alerting/check-rules/dash0-operator_.*_default_e2e-application-under-test-namespace_prometheus-rules-e2e-test_dash0%7Ck8s_0\\?dataset=default",
						"/api/alerting/check-rules/dash0-operator_.*_default_e2e-application-under-test-namespace_prometheus-rules-e2e-test_dash0%7Ck8s_1\\?dataset=default",
						"/api/alerting/check-rules/dash0-operator_.*_default_e2e-application-under-test-namespace_prometheus-rules-e2e-test_dash0%7Ccollector_0\\?dataset=default",
					}
					substrings := []string{
						"dash0/k8s - K8s Deployment replicas mismatch",
						"dash0/k8s - K8s pod crash looping",
						"dash0/collector - exporter send failed spans",
					}

					By("verifying the check rules have been synchronized to the Dash0 API via PUT")
					requests := fetchCapturedApiRequests(0, 4)
					Expect(requests).To(HaveLen(4))
					Expect(requests[0].Method).To(Equal("GET"))
					Expect(requests[0].Url).To(MatchRegexp(routeRegexes[0]))
					regexIdx := 1
					substringIdx := 0
					for i := 1; i < 4; i++ {
						req := requests[i]
						Expect(req.Method).To(Equal("PUT"))
						Expect(req.Url).To(MatchRegexp(routeRegexes[regexIdx]))
						regexIdx++
						Expect(req.Body).ToNot(BeNil())
						Expect(*req.Body).To(ContainSubstring(substrings[substringIdx]))
						substringIdx++
					}

					setOptOutLabelInPrometheusRule(applicationUnderTestNamespace, "false")
					By("verifying the check rules have been deleted via the Dash0 API (after setting dash0.com/enable=false)\"")
					requests = fetchCapturedApiRequests(4, 3)
					Expect(requests).To(HaveLen(3))
					regexIdx = 1
					for i := 0; i < 3; i++ {
						req := requests[i]
						Expect(req.Method).To(Equal("DELETE"))
						Expect(req.Url).To(MatchRegexp(routeRegexes[regexIdx]))
						regexIdx++
					}

					setOptOutLabelInPrometheusRule(applicationUnderTestNamespace, "true")
					By("verifying the check rules have been synchronized to the Dash0 API via PUT (after setting dash0.com/enable=true)")
					requests = fetchCapturedApiRequests(7, 4)
					Expect(requests).To(HaveLen(4))
					Expect(requests[0].Method).To(Equal("GET"))
					Expect(requests[0].Url).To(MatchRegexp(routeRegexes[0]))
					regexIdx = 1
					substringIdx = 0
					for i := 1; i < 4; i++ {
						req := requests[i]
						Expect(req.Method).To(Equal("PUT"))
						Expect(req.Url).To(MatchRegexp(routeRegexes[regexIdx]))
						regexIdx++
						Expect(*req.Body).To(ContainSubstring(substrings[substringIdx]))
						substringIdx++
					}

					removePrometheusRuleResource(applicationUnderTestNamespace)
					By("verifying the check rules have been deleted via the Dash0 API (after removing the resource)")
					requests = fetchCapturedApiRequests(11, 3)
					Expect(requests).To(HaveLen(3))
					regexIdx = 1
					for i := 0; i < 3; i++ {
						req := requests[i]
						Expect(req.Method).To(Equal("DELETE"))
						Expect(req.Url).To(MatchRegexp(routeRegexes[regexIdx]))
						regexIdx++
					}
				})
			})

			//nolint:lll
			It("config maps should not contain empty lines with space characters (that is, config maps should render nicely in k9s edit view)", func() {
				// See comment at the top of internal/collectors/otelcolresources/daemonset.config.yaml.template
				// This test looks for the problematic pattern (space character before line break) in the rendered
				// config maps.
				verifyDaemonSetCollectorConfigMapDoesNotContainStrings(operatorNamespace, " \n")
				verifyDeploymentCollectorConfigMapDoesNotContainStrings(operatorNamespace, " \n")
			})

		}) // end of suite "with an existing operator deployment and operation configuration resource::with a deployed
		// Dash0 monitoring resource"

		Describe("without a deployed Dash0 monitoring resource", func() {
			AfterEach(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
			})

			Describe("when instrumenting existing workloads", func() {

				It("should instrument and uninstrument all workload types", func() {
					testIds := make(map[string]string)
					workloadTestConfigs := workloadTestConfigs()
					for _, c := range workloadTestConfigs {
						mapKey := fmt.Sprintf(
							"%s-%s",
							c.runtime.runtimeTypeLabel,
							c.workloadType.workloadTypeString,
						)
						testIds[mapKey] = generateTestId(c.runtime, c.workloadType)
					}

					By("deploying all workloads")
					runInParallel(workloadTestConfigs, func(c workloadTestConfig) {
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

					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
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
					// attribute. See comment in spans.go#workloadSpansResourceMatcher.
					runInParallel(workloadTestConfigs, func(c workloadTestConfig) {
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
							true,
						)
					})
					By("all workloads have been instrumented")

					undeployDash0MonitoringResource(applicationUnderTestNamespace)

					// Deliberately kill any running cronjob or job pods, plus jobs spawned by a cronjob: There
					// could be old job/cronjob pods still running, those do not get restarted automatically, and
					// they could still produce spans, because instrumentation was active at the time they were
					// created.
					killBatchJobsAndPods(applicationUnderTestNamespace)

					runInParallel(workloadTestConfigs, func(c workloadTestConfig) {
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
				})
			})

			Describe("when it detects existing jobs or ownerless pods", func() {
				It("should label immutable jobs accordingly", func() {
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeJob)
					By("installing the Node.js job")
					Expect(installNodeJsJob(applicationUnderTestNamespace, testId)).To(Succeed())
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
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
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
					)
					By("verifying that the Node.js pod has not been labelled")
					Eventually(func(g Gomega) {
						verifyNoDash0Labels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypePod)
					}, labelChangeTimeout, pollingInterval).Should(Succeed())
				})
			})

			Describe("when attempting to revert the instrumentation for jobs", func() {
				// For all other workload types, reverting the instrumentation is tested by the controller test
				// suite. But the controller cannot instrument jobs, so we cannot test the (failing)
				// uninstrumentation procedure there. Thus, for jobs, we test the failing uninstrumentation and
				// its effects here separately.

				BeforeAll(func() {
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
					)
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
						true,
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

			Describe("when updating the Dash0Monitoring resource", func() {
				It("should instrument workloads when the Dash0Monitoring resource is switched from instrumentWorkloads.mode=none to instrumentWorkloads.mode=all", func() { //nolint
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeStatefulSet)
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValues{
							Endpoint:                defaultEndpoint,
							Token:                   defaultToken,
							InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeNone,
						},
						operatorNamespace,
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
						dash0common.InstrumentWorkloadsModeAll,
					)

					By("verifying that the Node.js stateful set has been instrumented by the controller")
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeStatefulSet,
						testId,
						images,
						"controller",
						true,
					)
				})

				It("should revert an instrumented workload when the Dash0Monitoring resource is switched from instrumentWorkloads.mode=all to instrumentWorkloads.mode=none", func() { //nolint
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
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
						true,
					)

					By("updating the Dash0Monitoring resource to instrumentWorkloads.mode=none")
					updateInstrumentWorkloadsModeOfDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0common.InstrumentWorkloadsModeNone,
					)

					verifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeDeployment,
						testId,
						"controller",
					)
				})
			})

			Describe("with a custom auto-instrumentation label selector", func() {

				BeforeAll(func() {
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValues{
							InstrumentWorkloadsLabelSelector: "instrument-with-dash0=yes",
						},
						operatorNamespace,
					)
				})

				AfterAll(func() {
					undeployDash0MonitoringResource(applicationUnderTestNamespace)
				})

				It("should instrument workloads that match the label selector", func() {
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeStatefulSet)

					By("installing the Node.js daemon set with label instrument-with-dash0: yes")
					// The daemonset in daemonset.opt-out.yaml also has the instrument-with-dash0=true label, so it can
					// be used for this test case.
					Expect(installNodeJsDaemonSetWithOptOutLabel(applicationUnderTestNamespace)).To(Succeed())
					By("verifying that the Node.js daemon set has been instrumented by the webhook")
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeDaemonSet,
						testId,
						images,
						"webhook",
						true,
					)
				})

				It("should not instrument workloads that do not match the label selector", func() {
					By("installing the Node.js daemon set")
					Expect(installNodeJsDaemonSet(applicationUnderTestNamespace)).To(Succeed())
					By("verifying that the Node.js daemon set has not been instrumented")
					Consistently(func(g Gomega) {
						verifyNoDash0Labels(g, applicationUnderTestNamespace, runtimeTypeNodeJs, workloadTypeDaemonSet)
					}, 10*time.Second, pollingInterval).Should(Succeed())
				})
			})

			Describe("self-monitoring telemetry", func() {

				It("should produce self-monitoring telemetry", func() {
					// Deploying the Dash0 monitoring resource  will trigger a metric data point for
					// dash0.operator.manager.monitoring.reconcile_requests to be produced.
					timestampLowerBound := time.Now()
					deployDash0MonitoringResource(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
					)

					By("waiting for self-monitoring metrics")
					Eventually(func(g Gomega) {
						verifyOperatorSelfMonitoringMetrics(g, timestampLowerBound)
						verifyCollectorSelfMonitoringMetrics(g, timestampLowerBound)
					}, 90*time.Second, time.Second).Should(Succeed())
				})
			})

			Describe("when using the v1alpha1 version of the monitoring resource", func() {

				It("should instrument and uninstrument workloads", func() {
					testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)

					By("installing the Node.js deployment")
					Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

					By("deploying the v1alpha1 Dash0 monitoring resource")
					deployDash0MonitoringResourceV1Alpha1(
						applicationUnderTestNamespace,
						dash0MonitoringValuesDefault,
						operatorNamespace,
					)

					By("verifying that the Node.js workload has been instrumented by the controller")
					verifyThatWorkloadHasBeenInstrumented(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeDeployment,
						testId,
						images,
						"controller",
						true,
					)
					By("removing the Dash0 monitoring resource")
					undeployDash0MonitoringResource(applicationUnderTestNamespace)
					By("verifying that the Node.js deployment has been uninstrumented")
					verifyThatInstrumentationHasBeenReverted(
						applicationUnderTestNamespace,
						runtimeTypeNodeJs,
						workloadTypeDeployment,
						testId,
						"controller",
					)
				})
			})

			Describe("operator manager memory consumption / memory leak test", func() {
				It("verify operator manager memory consumption is reasonable", func() {
					// This test's validity hinges to some degree on the fact that other tests have been executed
					// previously, using the same operator manager pod without restarting/redeploying it. Thus, a
					// memory leak would be visible by now. If an OOMKill had occurred before getting here, the
					// failOnPodCrashOrOOMKill check would have caught it.
					verifyOperatorManagerPodMemoryUsageIsReasonable()
				})
			})

		}) // end of suite "with an existing operator deployment and operation configuration resource::without a
		// deployed Dash0 monitoring resource"

	}) // end of suite "with an existing operator deployment and operation configuration resource"

	Describe("with an existing operator deployment without an operation configuration resource", func() {
		BeforeAll(func() {
			By("deploying the Dash0 operator")
			deployOperatorWithoutAutoOperationConfiguration(
				operatorNamespace,
				operatorHelmChart,
				operatorHelmChartUrl,
				images,
				nil,
			)
		})

		AfterAll(func() {
			undeployOperator(operatorNamespace)
		})

		Describe("using the monitoring resource's connection settings", func() {
			BeforeAll(func() {
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesWithExport,
					operatorNamespace,
				)
			})

			AfterAll(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
			})

			It("should instrument workloads and send telemetry to the endpoint configured in the monitoring resource", func() {
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
					false,
				)
			})
		})

		Describe("metrics collection", func() {
			var timestampLowerBound time.Time

			// Note: This test case deliberately works without an operator configuration resource, instead only using a
			// monitoring resource to enable the namespace for metrics monitoring _and_ the export. If we deployed an
			// operator configuration resource first, that would create a collector config map with the filter
			// discarding metrics from unmonitored namespaces set to allow only non-namepace scoped metrics (because
			// no namespace is monitored), and start the collector. Then, when we deploy the monitoring resource, the
			// config map would be updated, but then it takes a bit until the collector reloads its configuration via
			// the configuration reloader container. We can avoid that config change and the config reloading wait time
			// by skipping the operator configuration resource.

			BeforeAll(func() {
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesWithExport,
					operatorNamespace,
				)
				timestampLowerBound = time.Now()
			})

			AfterAll(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
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
		})

	}) // end of suite "with an existing operator deployment without an operation configuration resource"

	Describe("without an existing operator deployment", func() {

		Describe("collect basic metrics without having a Dash0 monitoring resource ", func() {

			var timestampLowerBound time.Time

			BeforeAll(func() {
				By("deploying the Dash0 operator and let it create an operator configuration resource")
				deployOperatorWithDefaultAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					// We are verifying that no namespaced metrics are collected later on in this test, but
					// self-monitoring metrics are namespaced, so we are deliberately disabling self-monitoring for this
					// test.
					false,
					nil,
				)
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())
				time.Sleep(10 * time.Second)
				timestampLowerBound = time.Now()
			})

			AfterAll(func() {
				undeployOperator(operatorNamespace)
			})

			It("should collect metrics without deploying a Dash0 monitoring resource", func() {
				By("waiting for metrics")
				Eventually(func(g Gomega) {
					verifyNonNamespaceScopedKubeletStatsMetricsOnly(g, timestampLowerBound)
				}, 50*time.Second, time.Second).Should(Succeed())
			})
		})

		Describe("telemetry filtering", func() {
			// Note: This test case deliberately works without an operator configuration resource, instead only using a
			// monitoring resource to configure the namespace telemetry filter _and_ the export. If we deployed an
			// operator configuration resource first, that would create a collector config map without any custom
			// filters, and start the collector. Then, when we deploy the monitoring resource, the config map would be
			// updated, but then it takes a bit until the collector is actually restarted due to the issue fixed in
			// https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 (which is not merged at the time of
			// writing), because the restart is blocked/delayed by the collector not being able to offload its
			// self-monitoring logs. We can avoid that config change and the config reloading wait time by skipping the
			// operator configuration resource.
			// We should be able to move this suite to the suite
			// "with an existing operator deployment and operation configuration resource::without deployed Dash0
			//  monitoring resource"
			// after https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 has been merged and released.

			BeforeEach(func() {
				deployOperatorWithoutAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					nil,
				)
			})

			AfterEach(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				undeployOperator(operatorNamespace)
			})

			It("emits health check spans without filter", func() {
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesWithExport,
					operatorNamespace,
				)
				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				testId := uuid.New().String()
				timestampLowerBound := time.Now()
				By("verifying that the Node.js deployment emits spans")
				Eventually(func(g Gomega) {
					verifySpans(
						g,
						runtimeTypeNodeJs,
						workloadTypeDeployment,
						testEndpoint,
						fmt.Sprintf("id=%s", testId),
						timestampLowerBound,
						false,
					)
				}, verifyTelemetryTimeout, pollingInterval).Should(Succeed())
				By("Node.js deployment: matching spans have been received")
				By("now searching collected spans for health checks...")
				matchResults := fileHasMatchingSpan(
					Default,
					nil,
					matchHttpServerSpanWithHttpTarget("/ready", ""),
					timestampLowerBound,
				)
				matchResults.expectAtLeastOneMatch(
					Default, "Node.js deployment: expected to find /ready check spans")
			})

			It("does not emit health check spans when filter is active", func() {
				filter :=
					`
traces:
  span:
  - 'attributes["http.route"] == "/ready"'
`
				// minTimestampCollectorRestart := time.Now()
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
						Endpoint:                defaultEndpoint,
						Token:                   defaultToken,
						Filter:                  filter,
					},
					operatorNamespace,
				)
				verifyDaemonSetCollectorConfigMapContainsString(
					operatorNamespace,
					// nolint:lll
					`- 'resource.attributes["k8s.namespace.name"] == "e2e-application-under-test-namespace" and (attributes["http.route"] == "/ready")'`,
				)
				// TODO reactivate this once we move these tests to the suite
				// "with an existing operator deployment and operation configuration resource::without deployed Dash0
				//  monitoring resource"
				// (waits for https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 to be merged and
				// released)
				// By("verify that the collector has restarted after the config change")
				// Eventually(func(g Gomega) {
				// 	mostRecentCollectorReadyTimeStamp := findMostRecentCollectorReadyLogLine(g)
				// 	g.Expect(mostRecentCollectorReadyTimeStamp).To(BeTemporally(">", minTimestampCollectorRestart))
				// }, 30*time.Second, time.Second).Should(Succeed())

				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				testId := uuid.New().String()
				timestampLowerBound := time.Now()
				By("verifying that the Node.js deployment emits spans")
				Eventually(func(g Gomega) {
					verifySpans(
						g,
						runtimeTypeNodeJs,
						workloadTypeDeployment,
						testEndpoint,
						fmt.Sprintf("id=%s", testId),
						timestampLowerBound,
						false,
					)
				}, verifyTelemetryTimeout, pollingInterval).Should(Succeed())
				By("Node.js deployment: matching spans have been received")
				By("now searching collected spans for health checks...")
				matchResults := fileHasMatchingSpan(
					Default,
					nil,
					matchHttpServerSpanWithHttpTarget("/ready", ""),
					timestampLowerBound,
				)
				matchResults.expectZeroMatches(
					Default, "Node.js deployment: expected to find no /ready check spans")
			})
		})

		Describe("telemetry transformation", func() {
			// Note: This test case deliberately works without an operator configuration resource, instead only using a
			// monitoring resource to configure the namespace telemetry filter _and_ the export. If we deployed an
			// operator configuration resource first, that would create a collector config map without any custom
			// filters, and start the collector. Then, when we deploy the monitoring resource, the config map would be
			// updated, but then it takes a bit until the collector is actually restarted due to the issue fixed in
			// https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 (which is not merged at the time of
			// writing), because the restart is blocked/delayed by the collector not being able to offload its
			// self-monitoring logs. We can avoid that config change and the config reloading wait time by skipping the
			// operator configuration resource.
			// We should be able to move this suite to the suite
			// "with an existing operator deployment and operation configuration resource::without deployed Dash0
			//  monitoring resource"
			// after https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 has been merged and released.

			BeforeEach(func() {
				deployOperatorWithoutAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					nil,
				)
			})

			AfterEach(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				undeployOperator(operatorNamespace)
			})

			It("truncates attributes when the transform is active", func() {
				transform :=
					`
trace_statements:
- truncate_all(span.attributes, 10)
`
				// minTimestampCollectorRestart := time.Now()
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
						Endpoint:                defaultEndpoint,
						Token:                   defaultToken,
						Transform:               transform,
					},
					operatorNamespace,
				)
				verifyDaemonSetCollectorConfigMapContainsString(
					operatorNamespace,
					`- 'truncate_all(span.attributes, 10)'`,
				)
				verifyDaemonSetCollectorConfigMapContainsString(
					operatorNamespace,
					`- 'resource.attributes["k8s.namespace.name"] == "e2e-application-under-test-namespace"'`,
				)
				// TODO reactivate this once we move these tests to the suite
				// "with an existing operator deployment and operation configuration resource::without deployed Dash0
				//  monitoring resource"
				// (waits for https://github.com/open-telemetry/opentelemetry-go-contrib/pull/6984 to be merged and
				// released)
				// By("verify that the collector has restarted after the config change")
				// Eventually(func(g Gomega) {
				//	 mostRecentCollectorReadyTimeStamp := findMostRecentCollectorReadyLogLine(g)
				//	 g.Expect(mostRecentCollectorReadyTimeStamp).To(BeTemporally(">", minTimestampCollectorRestart))
				// }, 30*time.Second, time.Second).Should(Succeed())

				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				testId := uuid.New().String()
				timestampLowerBound := time.Now()
				By("verifying that span attributes have been transformed")
				Eventually(func(g Gomega) {
					route := testEndpoint
					query := fmt.Sprintf("id=%s", testId)
					sendRequest(g, runtimeTypeNodeJs, workloadTypeDeployment, route, query)
					resourceMatchFn :=
						workloadSpansResourceMatcher(runtimeTypeNodeJs, workloadTypeDeployment, false)
					spanMatchFn := func(span ptrace.Span, matchResult *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]) {
						if span.Kind() == ptrace.SpanKindServer {
							matchResult.addPassedAssertion(spanKindKey)
						} else {
							matchResult.addFailedAssertion(
								spanKindKey,
								fmt.Sprintf("expected a server span, this span has kind \"%s\"", span.Kind().String()),
							)
						}

						truncatedRoute := route[0:10]
						truncatedQuery := query[0:10]

						target, hasTarget := span.Attributes().Get(httpTargetAttrib)
						route, hasRoute := span.Attributes().Get(httpRouteAttrib)
						query, hasQuery := span.Attributes().Get(urlQueryAttrib)
						if hasTarget {
							if target.Str() == truncatedRoute {
								matchResult.addPassedAssertion(httpTargetAttrib)
							} else {
								matchResult.addFailedAssertion(
									httpTargetAttrib,
									fmt.Sprintf("expected %s but it was %s", truncatedRoute, target.Str()),
								)
							}
						} else if hasRoute && hasQuery {
							if route.Str() == truncatedRoute && query.Str() == truncatedQuery {
								matchResult.addPassedAssertion(httpRouteAttrib + " and " + urlQueryAttrib)
							} else {
								matchResult.addFailedAssertion(
									httpRouteAttrib+" and "+urlQueryAttrib,
									fmt.Sprintf("expected %s & %s but it was %s & %s", truncatedRoute, truncatedQuery, route.Str(), query.Str()),
								)
							}
						} else {
							matchResult.addFailedAssertion(
								httpTargetAttrib+" or ("+httpRouteAttrib+" and "+urlQueryAttrib+")",
								fmt.Sprintf(
									"expected %s or (%s and %s) but the span had no such attributes",
									truncatedRoute,
									truncatedRoute,
									truncatedQuery,
								),
							)
						}
					}
					allMatchResults :=
						fileHasMatchingSpan(
							g,
							resourceMatchFn,
							spanMatchFn,
							timestampLowerBound,
						)
					allMatchResults.expectAtLeastOneMatch(g,
						"Node.js deployment: expected to find at least one matching HTTP server span with truncated attributes",
					)
				}, verifyTelemetryTimeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("with operatorConfiguration.telemetryCollection.enabled=false", func() {

			var timestampLowerBound time.Time

			BeforeAll(func() {
				By("deploying the Dash0 operator")
				deployOperatorWithoutAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					nil,
				)
				By("create an operator configuration resource with telemetryCollection.enabled=false")
				deployDash0OperatorConfigurationResource(dash0OperatorConfigurationValues{
					SelfMonitoringEnabled:      false,
					Endpoint:                   defaultEndpoint,
					Token:                      defaultToken,
					ApiEndpoint:                dash0ApiMockServiceBaseUrl,
					ClusterName:                e2eKubernetesContext,
					TelemetryCollectionEnabled: false,
				}, operatorNamespace, operatorHelmChart)
				time.Sleep(5 * time.Second)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeNone,
					},
					operatorNamespace,
				)
				time.Sleep(5 * time.Second)
				timestampLowerBound = time.Now()
			})

			AfterAll(func() {
				undeployOperator(operatorNamespace)
			})

			It("should not collect any telemetry", func() {
				By("verifying that no metrics are collected")
				Consistently(func(g Gomega) {
					verifyNoMetricsAtAll(g, timestampLowerBound)
				}, 10*time.Second, pollingInterval).Should(Succeed())
				verifyThatCollectorIsNotDeployed(operatorNamespace)
			})
		})

		Describe("when using cert-manager instead of auto-generated certs", func() {

			BeforeAll(func() {
				ensureCertManagerIsInstalled()
				ensureNamespaceExists(operatorNamespace)
				deployCertificateAndIssuer(operatorNamespace)

				By("deploying the Dash0 operator with useCertManager enabled")
				deployOperatorWithDefaultAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					true,
					map[string]string{
						"operator.certManager.useCertManager": "true",
						"operator.certManager.secretName":     "e2e-certificate-secret",
						//nolint:lll
						"operator.certManager.certManagerAnnotations.cert-manager\\.io/inject-ca-from": operatorNamespace + "/e2e-serving-certificate",
						"operator.webhookService.name": "e2e-webhook-service-name",
					},
				)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesDefault,
					operatorNamespace,
				)
			})

			AfterAll(func() {
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				undeployOperator(operatorNamespace)
				removeCertificateAndIssuer(operatorNamespace)
				uninstallCertManagerIfApplicable()
			})

			It("should instrument and uninstrument workloads via the webhook", func() {
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)

				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				By("verifying that the Node.js workload has been instrumented by the webhook")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					images,
					"webhook",
					true,
				)

				By("removing the Dash0 monitoring resource")
				undeployDash0MonitoringResource(applicationUnderTestNamespace)
				By("verifying that the Node.js deployment has been uninstrumented")
				verifyThatInstrumentationHasBeenReverted(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					"controller",
				)
			})
		})

		Describe("operator startup", func() {
			AfterAll(func() {
				undeployOperator(operatorNamespace)
			})

			It("should update instrumentation modifications at startup", func() {
				testId := generateTestId(runtimeTypeNodeJs, workloadTypeDeployment)
				By("installing the Node.js deployment")
				Expect(installNodeJsDeployment(applicationUnderTestNamespace)).To(Succeed())

				// We initially deploy the operator with alternative image names to simulate the workloads having
				// been instrumented by outdated images. Then (later) we will redeploy the operator with the actual
				// image names that are used throughout the whole test suite (defined by environment variables).

				initialAlternativeImages := Images{
					operator:                     deriveAlternativeImageForUpdateTest(images.operator),
					instrumentation:              deriveAlternativeImageForUpdateTest(images.instrumentation),
					collector:                    deriveAlternativeImageForUpdateTest(images.collector),
					configurationReloader:        deriveAlternativeImageForUpdateTest(images.configurationReloader),
					fileLogOffsetSync:            deriveAlternativeImageForUpdateTest(images.fileLogOffsetSync),
					fileLogOffsetVolumeOwnership: deriveAlternativeImageForUpdateTest(images.fileLogOffsetVolumeOwnership),
				}
				deployOperatorWithDefaultAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					initialAlternativeImages,
					true,
					nil,
				)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValuesDefault,
					operatorNamespace,
				)

				By("verifying that the Node.js deployment has been instrumented by the controller")
				verifyThatWorkloadHasBeenInstrumented(
					applicationUnderTestNamespace,
					runtimeTypeNodeJs,
					workloadTypeDeployment,
					testId,
					initialAlternativeImages,
					"controller",
					true,
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
					true,
				)
			})
		})

		Describe("when updating the Dash0OperatorConfiguration resource", func() {
			BeforeAll(func() {
				By("deploy the Dash0 operator")
				deployOperatorWithoutAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					nil,
				)
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
			It("should update the daemon set collector configuration when updating the Dash0 endpoint in the operator configuration resource", func() {
				deployDash0OperatorConfigurationResource(dash0OperatorConfigurationValues{
					SelfMonitoringEnabled:      false,
					Endpoint:                   defaultEndpoint,
					Token:                      defaultToken,
					ApiEndpoint:                dash0ApiMockServiceBaseUrl,
					ClusterName:                e2eKubernetesContext,
					TelemetryCollectionEnabled: true,
				}, operatorNamespace, operatorHelmChart)
				deployDash0MonitoringResource(
					applicationUnderTestNamespace,
					dash0MonitoringValues{
						InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
						Endpoint:                "",
						Token:                   "",
					},
					operatorNamespace,
				)

				By("waiting for the collector to be ready")
				var firstCollectorReadyTimeStamp time.Time
				Eventually(func(g Gomega) {
					firstCollectorReadyTimeStamp = findMostRecentCollectorReadyLogLine(g)
				}, 30*time.Second, time.Second).Should(Succeed())

				By("updating the Dash0 operator configuration endpoint setting")
				newEndpoint := "ingress.eu-east-1.aws.dash0-dev.com:4317"
				updateEndpointOfDash0OperatorConfigurationResource(newEndpoint)

				By("verify that the config map has been updated by the controller")
				verifyDaemonSetCollectorConfigMapContainsString(operatorNamespace, newEndpoint)

				By("verify that the configuration reloader says to have triggered a config change")
				verifyCollectorContainerLogContainsStrings(
					operatorNamespace,
					"configuration-reloader",
					10*time.Second,
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

				By("verify that the collector has restarted and has become ready once more")
				Eventually(func(g Gomega) {
					secondCollectorReadyTimeStamp := findMostRecentCollectorReadyLogLine(g)
					g.Expect(secondCollectorReadyTimeStamp).To(BeTemporally(">", firstCollectorReadyTimeStamp))
				}, 30*time.Second, time.Second).Should(Succeed())
			})
		})

		Describe("when deleting the Dash0OperatorConfiguration resource", func() {
			BeforeAll(func() {
				By("deploy the Dash0 operator")
				deployOperatorWithoutAutoOperationConfiguration(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					nil,
				)
				time.Sleep(10 * time.Second)
			})

			AfterAll(func() {
				undeployDash0OperatorConfigurationResource()
				undeployOperator(operatorNamespace)
			})

			//nolint:lll
			It("should remove the OpenTelemetry collector", func() {
				deployDash0OperatorConfigurationResource(dash0OperatorConfigurationValues{
					SelfMonitoringEnabled:      false,
					Endpoint:                   defaultEndpoint,
					Token:                      defaultToken,
					ApiEndpoint:                dash0ApiMockServiceBaseUrl,
					ClusterName:                e2eKubernetesContext,
					TelemetryCollectionEnabled: true,
				}, operatorNamespace, operatorHelmChart)

				undeployDash0OperatorConfigurationResource()

				verifyThatCollectorHasBeenRemoved(operatorNamespace)
			})
		})

		Describe("operator installation", func() {

			It("should fail if asked to create an operator configuration resource with invalid settings", func() {
				err := deployOperator(
					operatorNamespace,
					operatorHelmChart,
					operatorHelmChartUrl,
					images,
					&startup.OperatorConfigurationValues{
						Endpoint: util.EndpointDash0Test,
						// no token, no secret ref
					},
					nil,
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

					deployOperatorWithDefaultAutoOperationConfiguration(
						operatorNamespace,
						operatorHelmChart,
						operatorHelmChartUrl,
						images,
						true,
						nil,
					)
					runInParallel(configs, func(config removalTestNamespaceConfig) {
						deployDash0MonitoringResource(
							config.namespace,
							dash0MonitoringValuesDefault,
							operatorNamespace,
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
							true,
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

	}) // end of suite "without an existing operator deployment"
})

type workloadTestConfig struct {
	workloadType workloadType
	runtime      runtimeType
}

func (c workloadTestConfig) GetWorkloadType() workloadType {
	return c.workloadType
}

func (c workloadTestConfig) GetRuntimeType() runtimeType {
	return c.runtime
}

func workloadTestConfigs() []workloadTestConfig {
	return []workloadTestConfig{
		{workloadType: workloadTypeCronjob, runtime: runtimeTypeNodeJs},
		{workloadType: workloadTypeDaemonSet, runtime: runtimeTypeNodeJs},
		{workloadType: workloadTypeDeployment, runtime: runtimeTypeNodeJs},
		{workloadType: workloadTypeDeployment, runtime: runtimeTypeJvm},
		{workloadType: workloadTypeDeployment, runtime: runtimeTypeDotnet},
		{workloadType: workloadTypeReplicaSet, runtime: runtimeTypeNodeJs},
		{workloadType: workloadTypeStatefulSet, runtime: runtimeTypeNodeJs},
	}
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
	uninstallOtlpSink(workingDir, &cleanupSteps)
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

	images.fileLogOffsetSync.repository = getEnvOrDefault(
		"FILELOG_OFFSET_SYNC_IMG_REPOSITORY",
		images.fileLogOffsetSync.repository,
	)
	images.fileLogOffsetSync.tag = getEnvOrDefault("FILELOG_OFFSET_SYNC_IMG_TAG", images.fileLogOffsetSync.tag)
	images.fileLogOffsetSync.digest = getEnvOrDefault("FILELOG_OFFSET_SYNC_IMG_DIGEST",
		images.fileLogOffsetSync.digest)
	images.fileLogOffsetSync.pullPolicy = getEnvOrDefault(
		"FILELOG_OFFSET_SYNC_IMG_PULL_POLICY",
		images.fileLogOffsetSync.pullPolicy,
	)

	images.fileLogOffsetVolumeOwnership.repository = getEnvOrDefault(
		"FILELOG_OFFSET_VOLUME_OWNERSHIP_IMG_REPOSITORY",
		images.fileLogOffsetVolumeOwnership.repository,
	)
	images.fileLogOffsetVolumeOwnership.tag =
		getEnvOrDefault("FILELOG_OFFSET_VOLUME_OWNERSHIP_IMG_TAG", images.fileLogOffsetVolumeOwnership.tag)
	images.fileLogOffsetVolumeOwnership.digest =
		getEnvOrDefault("FILELOG_OFFSET_VOLUME_OWNERSHIP_IMG_DIGEST",
			images.fileLogOffsetVolumeOwnership.digest)
	images.fileLogOffsetVolumeOwnership.pullPolicy = getEnvOrDefault(
		"FILELOG_OFFSET_VOLUME_OWNERSHIP_IMG_PULL_POLICY",
		images.fileLogOffsetVolumeOwnership.pullPolicy,
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
		if isRemoteImage(images.fileLogOffsetSync) {
			images.fileLogOffsetSync.pullPolicy = getEnvOrDefault("FILELOG_OFFSET_SYNC_IMG_PULL_POLICY", "")
		}
		if isRemoteImage(images.fileLogOffsetVolumeOwnership) {
			images.fileLogOffsetVolumeOwnership.pullPolicy =
				getEnvOrDefault("FILELOG_OFFSET_VOLUME_OWNERSHIP_IMG_PULL_POLICY", "")
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
