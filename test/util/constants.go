// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"
)

const (
	TestNamespaceName      = "test-namespace"
	Dash0OperatorNamespace = "dash0-system"
	CronJobNamePrefix      = "cronjob"
	DaemonSetNamePrefix    = "daemonset"
	DeploymentNamePrefix   = "deployment"
	JobNamePrefix          = "job"
	PodNamePrefix          = "pod"
	ReplicaSetNamePrefix   = "replicaset"
	StatefulSetNamePrefix  = "statefulset"

	OperatorImageTest              = "some-registry.com:1234/dash0hq/operator-controller:1.2.3"
	InitContainerImageTest         = "some-registry.com:1234/dash0hq/instrumentation:4.5.6"
	CollectorImageTest             = "some-registry.com:1234/dash0hq/collector:7.8.9"
	ConfigurationReloaderImageTest = "some-registry.com:1234/dash0hq/configuration-reloader:10.11.12"
	FilelogOffsetSynchImageTest    = "some-registry.com:1234/dash0hq/filelog-offset-synch:13.14.15"

	OTelCollectorBaseUrlTest = "http://dash0-operator-opentelemetry-collector.dash0-system.svc.cluster.local:4318"
	EndpointTest             = "endpoint.dash0.com:4317"
	AuthorizationTokenTest   = "authorization-token"
	SecretRefTest            = "secret-ref"
	SecretRefEmpty           = ""
)

var (
	ArbitraryNumer int64 = 1302

	TestImages = util.Images{
		OperatorImage:                        OperatorImageTest,
		InitContainerImage:                   InitContainerImageTest,
		InitContainerImagePullPolicy:         corev1.PullAlways,
		CollectorImage:                       CollectorImageTest,
		CollectorImagePullPolicy:             corev1.PullAlways,
		ConfigurationReloaderImage:           ConfigurationReloaderImageTest,
		ConfigurationReloaderImagePullPolicy: corev1.PullAlways,
		FilelogOffsetSynchImage:              FilelogOffsetSynchImageTest,
		FilelogOffsetSynchImagePullPolicy:    corev1.PullAlways,
	}

	DeploymentSelfReference = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Dash0OperatorNamespace,
			Name:      "unit-test-dash0-operator-controller",
			UID:       "2f009c75-d69f-4b02-9d9d-fa17e76f5c1d",
		},
	}
)
