// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr/funcr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The filelog offset storage resolution", Ordered, func() {
	ctx := context.Background()

	podsResource := schema.GroupResource{Group: "", Resource: "pods"}
	defaultHostPath := "/var/lib/dash0-operator/filelog-offsets/" + OperatorNamespace
	userVolume := &corev1.Volume{
		Name: "custom-offset-volume",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "offset-storage-claim"},
		},
	}
	webhookDenial := apierrors.NewForbidden(podsResource, "", errors.New(
		"admission webhook \"validation.gatekeeper.sh\" denied the request: [psp-host-filesystem] HostPath volume "+
			"is not allowed"))
	serviceAccountNotFound := apierrors.NewForbidden(podsResource, "", errors.New(
		"error looking up service account dash0-system/unit-test-opentelemetry-collector-sa: serviceaccount "+
			"\"unit-test-opentelemetry-collector-sa\" not found"))

	var logged []string
	var logger logd.Logger

	BeforeAll(func() {
		EnsureOperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		logged = nil
		logger = logd.NewLogger(funcr.New(func(_ string, args string) {
			logged = append(logged, args)
		}, funcr.Options{}))
	})

	AfterEach(func() {
		for _, object := range []client.Object{
			&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: OperatorNamespace, Name: ExpectedDaemonSetName}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Namespace: OperatorNamespace, Name: ExpectedDaemonSetFilelogOffsetSyncConfigMapName,
			}},
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Namespace: OperatorNamespace, Name: ExpectedDaemonSetServiceAccountName,
			}},
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: OperatorNamespace, Name: "external-sa"}},
		} {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, object))).To(Succeed())
		}
	})

	newManagerWithCollectorConfig := func(
		k8sClient client.Client,
		modifyCollectorConfig func(*util.CollectorConfig),
	) *OTelColResourceManager {
		collectorConfig := util.CollectorConfig{
			Images:            TestImages,
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        ManagedResourceNamePrefixTest,
		}
		if modifyCollectorConfig != nil {
			modifyCollectorConfig(&collectorConfig)
		}
		return NewOTelColResourceManager(
			k8sClient,
			k8sClient.Scheme(),
			OperatorManagerDeployment,
			collectorConfig,
			NewLeaderElectionAwareMock(true),
		)
	}
	newManager := func(k8sClient client.Client) *OTelColResourceManager {
		return newManagerWithCollectorConfig(k8sClient, nil)
	}
	newConfig := func() *oTelColConfig {
		return &oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        ManagedResourceNamePrefixTest,
			Exporters:         defaultDash0ExportersWithToken(),
			Images:            TestImages,
		}
	}
	extraConfigWithMode := func(mode util.FilelogOffsetStorageMode) util.ExtraConfig {
		extraConfig := util.ExtraConfigDefaults
		extraConfig.CollectorFilelogOffsetStorageMode = mode
		return extraConfig
	}
	resolve := func(manager *OTelColResourceManager, config *oTelColConfig, extraConfig util.ExtraConfig) {
		GinkgoHelper()
		Expect(manager.resolveFilelogOffsetStorage(ctx, config, extraConfig, nil, logger)).To(Succeed())
	}
	expectHostPath := func(config *oTelColConfig, path string) {
		GinkgoHelper()
		Expect(config.OffsetStorageVolume).ToNot(BeNil())
		Expect(config.OffsetStorageVolume.Name).To(Equal("filelogreceiver-offsets"))
		Expect(config.OffsetStorageVolume.HostPath).ToNot(BeNil())
		Expect(config.OffsetStorageVolume.HostPath.Path).To(Equal(path))
		Expect(*config.OffsetStorageVolume.HostPath.Type).To(Equal(corev1.HostPathDirectoryOrCreate))
	}
	expectConfigMap := func(config *oTelColConfig) {
		GinkgoHelper()
		Expect(config.OffsetStorageVolume).To(BeNil())
		Expect(config.KeepFilelogOffsetsConfigMap).To(BeFalse())
		Expect(config.MigrateFilelogOffsetsFromConfigMap).To(BeFalse())
	}
	expectMigration := func(config *oTelColConfig, keepConfigMap bool, migrate bool) {
		GinkgoHelper()
		Expect(config.KeepFilelogOffsetsConfigMap).To(Equal(keepConfigMap))
		Expect(config.MigrateFilelogOffsetsFromConfigMap).To(Equal(migrate))
	}
	countLogged := func(fragment string) int {
		count := 0
		for _, line := range logged {
			if strings.Contains(line, fragment) {
				count++
			}
		}
		return count
	}

	createLiveDaemonSet := func(volume *corev1.Volume, withMigrateInitContainer bool) *appsv1.DaemonSet {
		GinkgoHelper()
		config := newConfig()
		config.OffsetStorageVolume = volume
		config.MigrateFilelogOffsetsFromConfigMap = withMigrateInitContainer
		daemonSet, err := assembleCollectorDaemonSet(config, util.ExtraConfigDefaults)
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sClient.Create(ctx, daemonSet)).To(Succeed())
		return daemonSet
	}
	markRollout := func(daemonSet *appsv1.DaemonSet, complete bool) {
		GinkgoHelper()
		available := int32(3)
		if !complete {
			available = 2
		}
		daemonSet.Status = appsv1.DaemonSetStatus{
			ObservedGeneration:     daemonSet.Generation,
			CurrentNumberScheduled: 3,
			DesiredNumberScheduled: 3,
			NumberReady:            available,
			UpdatedNumberScheduled: 3,
			NumberAvailable:        available,
			NumberUnavailable:      3 - available,
		}
		Expect(k8sClient.Status().Update(ctx, daemonSet)).To(Succeed())
	}
	createFilelogOffsetsConfigMap := func() {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: OperatorNamespace,
				Name:      ExpectedDaemonSetFilelogOffsetSyncConfigMapName,
			},
		})).To(Succeed())
	}

	Describe("with an explicit storage", func() {
		It("should use a user-provided volume regardless of the storage mode", func() {
			extraConfig := extraConfigWithMode(util.FilelogOffsetStorageModeConfigMap)
			extraConfig.CollectorFilelogOffsetStorageVolume = userVolume
			config := newConfig()
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, extraConfig)

			Expect(config.OffsetStorageVolume).To(Equal(userVolume))
			expectMigration(config, false, false)
			Expect(probeClient.probePods).To(BeEmpty())
		})

		It("should use the config map in mode configMap without probing", func() {
			config := newConfig()
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, extraConfigWithMode(util.FilelogOffsetStorageModeConfigMap))

			expectConfigMap(config)
			Expect(probeClient.probePods).To(BeEmpty())
		})

		It("should use the hostPath volume in mode hostPath without probing", func() {
			extraConfig := extraConfigWithMode(util.FilelogOffsetStorageModeHostPath)
			extraConfig.CollectorFilelogOffsetHostPath = "/data/offsets"
			config := newConfig()
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, extraConfig)

			expectHostPath(config, "/data/offsets/"+OperatorNamespace)
			expectMigration(config, false, false)
			Expect(probeClient.probePods).To(BeEmpty())
		})

		It("should use the default host path when the extra config has none", func() {
			extraConfig := extraConfigWithMode(util.FilelogOffsetStorageModeHostPath)
			extraConfig.CollectorFilelogOffsetHostPath = ""
			config := newConfig()

			resolve(newManager(k8sClient), config, extraConfig)

			expectHostPath(config, defaultHostPath)
		})

		It("should resolve nothing if the desired state contains no collector daemonset", func() {
			config := newConfig()
			config.SignalControlSceOnlyMode = true
			config.OffsetStorageVolume = userVolume
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(probeClient.probePods).To(BeEmpty())
			VerifyResourceDoesNotExist(
				ctx, k8sClient, OperatorNamespace, ExpectedDaemonSetServiceAccountName, &corev1.ServiceAccount{})
			Expect(logged).To(BeEmpty())
		})
	})

	Describe("in mode auto without probing", func() {
		It("should use the config map on GKE Autopilot", func() {
			config := newConfig()
			config.IsGkeAutopilot = true
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(probeClient.probePods).To(BeEmpty())
		})

		It("should use the config map on OpenShift if the pods are not spc_t", func() {
			config := newConfig()
			config.IsOpenShift = true
			for _, seLinuxOptions := range []*corev1.SELinuxOptions{nil, {Type: "container_t"}, {Level: "s0:c1,c2"}} {
				extraConfig := util.ExtraConfigDefaultsFor(false, true)
				extraConfig.DaemonSetSELinuxOptions = seLinuxOptions
				probeClient := &dryRunPodClient{Client: k8sClient}

				resolve(newManager(probeClient), config, extraConfig)

				expectConfigMap(config)
				Expect(probeClient.probePods).To(BeEmpty())
			}
		})

		It("should use the config map if the nodes indicate SELinux and the pods are not spc_t", func() {
			config := newConfig()
			probeClient := &dryRunPodClient{Client: k8sClient}
			manager := newManagerWithCollectorConfig(probeClient, func(collectorConfig *util.CollectorConfig) {
				collectorConfig.NodeSELinuxIndication = cluster.NodeSELinuxIndication{
					Indication: "the node node-1 runs RKE2 (kubelet version v1.33.4+rke2r1)",
				}
			})

			resolve(manager, config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(probeClient.probePods).To(BeEmpty())
			Expect(countLogged("the node node-1 runs RKE2")).To(Equal(1))
			Expect(countLogged("set operator.collectors.filelogOffsetStorageMode=hostPath")).To(Equal(1))
		})

		It("should keep the storage of the daemonset if the nodes could not be inspected for SELinux", func() {
			probeClient := &dryRunPodClient{Client: k8sClient}
			manager := newManagerWithCollectorConfig(probeClient, func(collectorConfig *util.CollectorConfig) {
				collectorConfig.NodeSELinuxIndication = cluster.NodeSELinuxIndication{InspectionFailed: true}
			})

			config := newConfig()
			resolve(manager, config, util.ExtraConfigDefaults)
			expectConfigMap(config)

			createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), false)
			config = newConfig()
			resolve(manager, config, util.ExtraConfigDefaults)
			expectHostPath(config, defaultHostPath)

			Expect(probeClient.probePods).To(BeEmpty())
			Expect(countLogged("could not be inspected for SELinux")).To(Equal(1))
		})
	})

	Describe("in mode auto with probing", func() {
		It("should probe on OpenShift if the pods are spc_t", func() {
			config := newConfig()
			config.IsOpenShift = true
			extraConfig := util.ExtraConfigDefaultsFor(false, true)
			Expect(extraConfig.DaemonSetSELinuxOptions).To(Equal(&corev1.SELinuxOptions{Type: "spc_t"}))
			probeClient := &dryRunPodClient{Client: k8sClient}

			resolve(newManager(probeClient), config, extraConfig)

			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(HaveLen(1))
		})

		It("should probe if the nodes indicate SELinux but the pods are spc_t", func() {
			config := newConfig()
			extraConfig := util.ExtraConfigDefaults
			extraConfig.DaemonSetSELinuxOptions = &corev1.SELinuxOptions{Type: "spc_t"}
			probeClient := &dryRunPodClient{Client: k8sClient}
			manager := newManagerWithCollectorConfig(probeClient, func(collectorConfig *util.CollectorConfig) {
				collectorConfig.NodeSELinuxIndication = cluster.NodeSELinuxIndication{
					Indication: "the node node-1 runs the container runtime CRI-O (cri-o://1.33.4)",
				}
			})

			resolve(manager, config, extraConfig)

			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(HaveLen(1))
		})

		It("should use the hostPath volume when the dry run is admitted, and cache the outcome", func() {
			probeClient := &dryRunPodClient{Client: k8sClient}
			manager := newManager(probeClient)
			config := newConfig()

			resolve(manager, config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
			expectMigration(config, false, false)
			Expect(probeClient.probePods).To(HaveLen(1))
			probePod := probeClient.probePods[0]
			Expect(probePod.GenerateName).To(Equal(ExpectedDaemonSetName + "-filelog-offset-probe-"))
			Expect(probePod.Spec.ServiceAccountName).To(Equal(ExpectedDaemonSetServiceAccountName))
			Expect(FindVolumeByName(probePod.Spec.Volumes, "filelogreceiver-offsets").HostPath.Path).
				To(Equal(defaultHostPath))
			Expect(probePod.Spec.InitContainers).To(HaveLen(2))
			Expect(probePod.Spec.InitContainers[0].Name).To(Equal("filelog-offset-volume-ownership"))
			Expect(probePod.Spec.InitContainers[1].Name).To(Equal("filelog-offset-migrate"))
			Expect(manager.filelogOffsetHostPathProbeResult.Load()).To(Equal(
				&hostPathProbeResult{hostPath: defaultHostPath, outcome: hostPathAdmitted}))
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList, client.InNamespace(OperatorNamespace))).To(Succeed())
			Expect(podList.Items).To(BeEmpty())

			config = newConfig()
			resolve(manager, config, util.ExtraConfigDefaults)
			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(HaveLen(1))
			Expect(countLogged("Storing the filelog receiver offsets")).To(Equal(1))
		})

		It("should create the service account of the daemonset before the dry run", func() {
			probeClient := &dryRunPodClient{Client: k8sClient}
			VerifyResourceDoesNotExist(
				ctx, k8sClient, OperatorNamespace, ExpectedDaemonSetServiceAccountName, &corev1.ServiceAccount{})

			resolve(newManager(probeClient), newConfig(), util.ExtraConfigDefaults)

			Expect(probeClient.serviceAccountExistedAtProbe).To(Equal([]bool{true}))
			serviceAccount := VerifyResourceExists(
				ctx, k8sClient, OperatorNamespace, ExpectedDaemonSetServiceAccountName, &corev1.ServiceAccount{})
			Expect(serviceAccount.GetOwnerReferences()).To(HaveLen(1))
			Expect(serviceAccount.GetOwnerReferences()[0].Name).To(Equal(OperatorManagerDeployment.Name))
			Expect(serviceAccount.GetLabels()).To(HaveKeyWithValue("app.kubernetes.io/name", "opentelemetry-collector"))
			Expect(serviceAccount.GetAnnotations()).To(HaveKey("argocd.argoproj.io/sync-options"))
		})

		It("should not probe if an external service account of the daemonset does not exist", func() {
			probeClient := &dryRunPodClient{Client: k8sClient}
			config := newConfig()
			config.DaemonSetServiceAccountName = "external-sa"

			resolve(newManager(probeClient), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(probeClient.probePods).To(BeEmpty())
			Expect(countLogged("the service account external-sa of the collector daemonset is not available")).
				To(Equal(1))

			Expect(k8sClient.Create(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Namespace: OperatorNamespace, Name: "external-sa"},
			})).To(Succeed())
			config = newConfig()
			config.DaemonSetServiceAccountName = "external-sa"

			resolve(newManager(probeClient), config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(HaveLen(1))
			Expect(probeClient.probePods[0].Spec.ServiceAccountName).To(Equal("external-sa"))
			VerifyResourceDoesNotExist(
				ctx, k8sClient, OperatorNamespace, ExpectedDaemonSetServiceAccountName, &corev1.ServiceAccount{})
		})

		It("should fall back to the config map when the dry run is denied, and cache the outcome", func() {
			probeClient := &dryRunPodClient{Client: k8sClient, err: webhookDenial}
			manager := newManager(probeClient)
			config := newConfig()

			resolve(manager, config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(manager.filelogOffsetHostPathProbeResult.Load()).To(Equal(&hostPathProbeResult{
				hostPath: defaultHostPath,
				outcome:  hostPathDenied,
				detail:   webhookDenial.Error(),
			}))
			Expect(countLogged("validation.gatekeeper.sh")).To(Equal(1))

			config = newConfig()
			resolve(manager, config, util.ExtraConfigDefaults)
			expectConfigMap(config)
			Expect(probeClient.probePods).To(HaveLen(1))
		})

		It("should probe again if the host path changes", func() {
			probeClient := &dryRunPodClient{Client: k8sClient, err: webhookDenial}
			manager := newManager(probeClient)

			resolve(manager, newConfig(), util.ExtraConfigDefaults)
			extraConfig := util.ExtraConfigDefaults
			extraConfig.CollectorFilelogOffsetHostPath = "/data/offsets"
			probeClient.err = nil
			config := newConfig()
			resolve(manager, config, extraConfig)

			expectHostPath(config, "/data/offsets/"+OperatorNamespace)
			Expect(probeClient.probePods).To(HaveLen(2))
		})

		It("should not cache an inconclusive outcome, and probe again on the next reconciliation", func() {
			probeClient := &dryRunPodClient{Client: k8sClient, err: serviceAccountNotFound}
			manager := newManager(probeClient)
			config := newConfig()

			resolve(manager, config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(manager.filelogOffsetHostPathProbeResult.Load()).To(BeNil())

			probeClient.err = nil
			config = newConfig()
			resolve(manager, config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(HaveLen(2))
		})

		It("should log an inconclusive outcome only when it is new or has changed", func() {
			probeClient := &dryRunPodClient{Client: k8sClient, err: apierrors.NewServerTimeout(podsResource, "create", 1)}
			manager := newManager(probeClient)

			for range 3 {
				resolve(manager, newConfig(), util.ExtraConfigDefaults)
			}
			Expect(countLogged("It could not be determined whether the cluster admits")).To(Equal(1))
			Expect(countLogged("Storing the filelog receiver offsets")).To(Equal(1))

			probeClient.err = apierrors.NewTooManyRequests("slow down", 1)
			resolve(manager, newConfig(), util.ExtraConfigDefaults)
			resolve(manager, newConfig(), util.ExtraConfigDefaults)
			Expect(countLogged("It could not be determined whether the cluster admits")).To(Equal(2))
			Expect(probeClient.probePods).To(HaveLen(5))
		})

		It("should reuse the cached outcome instead of probing again", func() {
			probeClient := &dryRunPodClient{Client: k8sClient, err: errors.New("no probe expected")}
			manager := newManager(probeClient)
			manager.filelogOffsetHostPathProbeResult.Store(
				&hostPathProbeResult{hostPath: defaultHostPath, outcome: hostPathAdmitted})
			config := newConfig()

			resolve(manager, config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
			Expect(probeClient.probePods).To(BeEmpty())
		})
	})

	Describe("stickiness with an inconclusive probe", func() {
		inconclusive := apierrors.NewInternalError(errors.New("etcdserver: request timed out"))

		It("should use the config map if there is no daemonset yet", func() {
			config := newConfig()
			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: inconclusive}), config, util.ExtraConfigDefaults)
			expectConfigMap(config)
		})

		It("should keep the hostPath volume if the daemonset uses it", func() {
			createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), false)
			config := newConfig()

			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: inconclusive}), config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
			expectMigration(config, false, false)
		})

		It("should keep the hostPath volume if the daemonset uses a hostPath volume with a different path", func() {
			createLiveDaemonSet(filelogOffsetHostPathVolume("/data/old-offsets", OperatorNamespace), false)
			config := newConfig()

			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: inconclusive}), config, util.ExtraConfigDefaults)

			expectHostPath(config, defaultHostPath)
		})

		It("should keep the config map if the daemonset uses it", func() {
			createLiveDaemonSet(nil, false)
			config := newConfig()

			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: inconclusive}), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(logged).ToNot(ContainElement(ContainSubstring("switched from a volume to the config map")))
		})

		It("should use the config map if the daemonset uses a volume that is no longer configured", func() {
			createLiveDaemonSet(userVolume, false)
			config := newConfig()

			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: inconclusive}), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(countLogged("switched from a volume to the config map")).To(Equal(1))
		})

		It("should switch from the hostPath volume to the config map on a conclusive denial, with a warning", func() {
			createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), true)
			config := newConfig()

			resolve(newManager(&dryRunPodClient{Client: k8sClient, err: webhookDenial}), config, util.ExtraConfigDefaults)

			expectConfigMap(config)
			Expect(countLogged("switched from a volume to the config map")).To(Equal(1))
			Expect(countLogged("log records may be lost or sent twice")).To(Equal(1))
		})
	})

	Describe("the lifecycle of the filelog offsets config map", func() {
		hostPathMode := extraConfigWithMode(util.FilelogOffsetStorageModeHostPath)

		It("should neither keep the config map nor migrate on a fresh installation", func() {
			config := newConfig()
			resolve(newManager(k8sClient), config, hostPathMode)
			expectHostPath(config, defaultHostPath)
			expectMigration(config, false, false)
		})

		It("should keep the config map and migrate if the daemonset still uses the config map", func() {
			createFilelogOffsetsConfigMap()
			daemonSet := createLiveDaemonSet(nil, false)
			markRollout(daemonSet, true)
			config := newConfig()

			resolve(newManager(k8sClient), config, hostPathMode)

			expectHostPath(config, defaultHostPath)
			expectMigration(config, true, true)
		})

		It("should keep the config map and migrate if there is a config map but no daemonset", func() {
			createFilelogOffsetsConfigMap()
			config := newConfig()

			resolve(newManager(k8sClient), config, hostPathMode)

			expectMigration(config, true, true)
		})

		It("should keep the config map and migrate while the rollout of the volume is not complete", func() {
			createFilelogOffsetsConfigMap()
			daemonSet := createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), true)
			config := newConfig()

			resolve(newManager(k8sClient), config, hostPathMode)
			expectMigration(config, true, true)

			markRollout(daemonSet, false)
			config = newConfig()
			resolve(newManager(k8sClient), config, hostPathMode)
			expectMigration(config, true, true)
		})

		It("should drop the config map but keep the migrate init container once the rollout of the volume is complete",
			func() {
				createFilelogOffsetsConfigMap()
				daemonSet := createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), true)
				markRollout(daemonSet, true)
				config := newConfig()

				resolve(newManager(k8sClient), config, hostPathMode)

				expectHostPath(config, defaultHostPath)
				expectMigration(config, false, true)
			})

		It("should keep the migrate init container after the config map has been deleted", func() {
			createLiveDaemonSet(filelogOffsetHostPathVolume("", OperatorNamespace), true)
			config := newConfig()

			resolve(newManager(k8sClient), config, hostPathMode)

			expectMigration(config, false, true)
		})

		It("should drop the config map without adding the migrate init container if the daemonset has rolled out "+
			"with a volume without it", func() {
			createFilelogOffsetsConfigMap()
			daemonSet := createLiveDaemonSet(userVolume, false)
			markRollout(daemonSet, true)
			extraConfig := util.ExtraConfigDefaults
			extraConfig.CollectorFilelogOffsetStorageVolume = userVolume
			config := newConfig()

			resolve(newManager(k8sClient), config, extraConfig)

			Expect(config.OffsetStorageVolume).To(Equal(userVolume))
			expectMigration(config, false, false)
		})

		It("should migrate from the config map into a user-provided volume", func() {
			createFilelogOffsetsConfigMap()
			daemonSet := createLiveDaemonSet(nil, false)
			markRollout(daemonSet, true)
			extraConfig := util.ExtraConfigDefaults
			extraConfig.CollectorFilelogOffsetStorageVolume = userVolume
			config := newConfig()

			resolve(newManager(k8sClient), config, extraConfig)

			Expect(config.OffsetStorageVolume).To(Equal(userVolume))
			expectMigration(config, true, true)
		})
	})

	DescribeTable("should classify the outcome of the dry run",
		func(err error, expected hostPathProbeOutcome) {
			Expect(classifyHostPathProbeError(err)).To(Equal(expected))
		},
		Entry("no error", nil, hostPathAdmitted),
		Entry("a validating webhook denial (OPA gatekeeper)", webhookDenial, hostPathDenied),
		Entry("a validating webhook denial (Kyverno)",
			apierrors.NewBadRequest("admission webhook \"validate.kyverno.svc-fail\" denied the request: \n\n"+
				"resource Pod/dash0-system/probe was blocked due to the following policies\n\n"+
				"disallow-host-path:\n  host-path: HostPath volumes are forbidden."),
			hostPathDenied),
		Entry("a mutating webhook denial with a custom status code",
			&apierrors.StatusError{ErrStatus: metav1.Status{
				Status:  metav1.StatusFailure,
				Code:    422,
				Message: "admission webhook \"mutate.example.com\" denied the request without explanation",
			}},
			hostPathDenied),
		Entry("pod security admission",
			apierrors.NewForbidden(podsResource, "probe", errors.New("violates PodSecurity \"baseline:latest\": "+
				"hostPath volumes (volumes \"filelogreceiver-offsets\", \"node-pod-logs\")")),
			hostPathDenied),
		Entry("OpenShift security context constraints",
			apierrors.NewForbidden(podsResource, "probe", errors.New("unable to validate against any security "+
				"context constraint: [provider \"restricted-v2\": Forbidden: not usable by user or serviceaccount]")),
			hostPathDenied),
		Entry("a validating admission policy with binding",
			apierrors.NewForbidden(podsResource, "probe", errors.New("ValidatingAdmissionPolicy 'no-host-path' "+
				"with binding 'no-host-path-binding' denied request: hostPath volumes are not allowed")),
			hostPathDenied),
		Entry("a validating admission policy without binding",
			apierrors.NewForbidden(podsResource, "probe", errors.New("ValidatingAdmissionPolicy 'no-host-path' "+
				"denied request: hostPath volumes are not allowed")),
			hostPathDenied),
		Entry("a missing service account", serviceAccountNotFound, hostPathProbeInconclusive),
		Entry("an exceeded resource quota",
			apierrors.NewForbidden(podsResource, "probe", errors.New("exceeded quota: compute-resources, "+
				"requested: pods=1, used: pods=10, limited: pods=10")),
			hostPathProbeInconclusive),
		Entry("missing permissions",
			apierrors.NewForbidden(podsResource, "", errors.New("User \"system:serviceaccount:dash0-system:"+
				"dash0-operator-controller\" cannot create resource \"pods\" in API group \"\" in the namespace "+
				"\"dash0-system\"")),
			hostPathProbeInconclusive),
		Entry("a webhook that does not support dry run",
			apierrors.NewBadRequest("admission webhook \"legacy.example.com\" does not support dry run"),
			hostPathProbeInconclusive),
		Entry("a webhook that cannot be reached",
			apierrors.NewInternalError(errors.New("failed calling webhook \"validate.kyverno.svc-fail\": "+
				"failed to call webhook: context deadline exceeded")),
			hostPathProbeInconclusive),
		Entry("an invalid pod", apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "probe", nil),
			hostPathProbeInconclusive),
		Entry("a server timeout", apierrors.NewServerTimeout(podsResource, "create", 1), hostPathProbeInconclusive),
		Entry("too many requests", apierrors.NewTooManyRequests("slow down", 1), hostPathProbeInconclusive),
		Entry("unauthorized", apierrors.NewUnauthorized("unauthorized"), hostPathProbeInconclusive),
		Entry("any other error", fmt.Errorf("connection refused"), hostPathProbeInconclusive),
	)
})

// dryRunPodClient answers the dry run of a pod with a fixed error, to simulate the admission outcome of the hostPath
// probe, or passes it on to the wrapped client if no error is set. It records the probe pods and whether the
// service account of each probe pod existed at the time of the dry run.
type dryRunPodClient struct {
	client.Client
	err                          error
	probePods                    []*corev1.Pod
	serviceAccountExistedAtProbe []bool
}

func (c *dryRunPodClient) Create(ctx context.Context, object client.Object, opts ...client.CreateOption) error {
	pod, isPod := object.(*corev1.Pod)
	if !isPod {
		return c.Client.Create(ctx, object, opts...)
	}
	c.probePods = append(c.probePods, pod.DeepCopy())
	serviceAccountErr := c.Get(
		ctx,
		client.ObjectKey{Namespace: pod.Namespace, Name: pod.Spec.ServiceAccountName},
		&corev1.ServiceAccount{},
	)
	c.serviceAccountExistedAtProbe = append(c.serviceAccountExistedAtProbe, serviceAccountErr == nil)
	if c.err != nil {
		return c.err
	}
	return c.Client.Create(ctx, object, opts...)
}
