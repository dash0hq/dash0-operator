// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	ClusterUIDTest              = "cluster-uid-test"
	ClusterNameTest             = "cluster-name-test"
	TestNamespaceName           = "test-namespace"
	OperatorNamespace           = "test-operator-namespace"
	OperatorPodName             = "test-operator-pod-name"
	OTelCollectorNamePrefixTest = "unit-test"

	CronJobNamePrefix     = "cronjob"
	DaemonSetNamePrefix   = "daemonset"
	DeploymentNamePrefix  = "deployment"
	JobNamePrefix         = "job"
	PodNamePrefix         = "pod"
	ReplicaSetNamePrefix  = "replicaset"
	StatefulSetNamePrefix = "statefulset"

	OperatorVersionTest            = "1.2.3"
	OperatorImageTest              = "some-registry.com:1234/dash0hq/operator-controller:1.2.3"
	InitContainerImageTest         = "some-registry.com:1234/dash0hq/instrumentation:4.5.6"
	CollectorImageTest             = "some-registry.com:1234/dash0hq/collector:7.8.9"
	ConfigurationReloaderImageTest = "some-registry.com:1234/dash0hq/configuration-reloader:10.11.12"
	FilelogOffsetSyncImageTest     = "some-registry.com:1234/dash0hq/filelog-offset-sync:13.14.15"

	OTelCollectorNodeLocalBaseUrlTest = "http://$(DASH0_NODE_IP):40318"
	OTelCollectorServiceBaseUrlTest   = //
	"http://unit-test-opentelemetry-collector-service.test-operator-namespace.svc.cluster.local:4318"
	EndpointDash0Test             = "endpoint.dash0.com:4317"
	EndpointDash0TestAlternative  = "endpoint-alternative.dash0.com:4317"
	EndpointDash0TestQuoted       = "\"endpoint.dash0.com:4317\""
	EndpointDash0WithProtocolTest = "https://endpoint.dash0.com:4317"
	EndpointGrpcTest              = "endpoint.backend.com:4317"
	EndpointGrpcWithProtocolTest  = "dns://endpoint.backend.com:4317"
	EndpointHttpTest              = "https://endpoint.backend.com:4318"

	ApiEndpointTest   = "https://api.dash0.com"
	DatasetCustomTest = "test-dataset"
)

var (
	AuthorizationTokenTest             = "authorization-token-test"
	AuthorizationHeaderTest            = fmt.Sprintf("Bearer %s", AuthorizationTokenTest)
	AuthorizationTokenTestAlternative  = "authorization-token-test-alternative"
	AuthorizationHeaderTestAlternative = fmt.Sprintf("Bearer %s", AuthorizationTokenTestAlternative)
	AuthorizationTokenTestFromSecret   = "authorization-token-test-from-secret"
	AuthorizationHeaderTestFromSecret  = fmt.Sprintf("Bearer %s", AuthorizationTokenTestFromSecret)
	SecretRefTest                      = dash0common.SecretRef{
		Name: "secret-ref",
		Key:  "key",
	}

	ArbitraryNumer int64 = 1302

	TestImages = util.Images{
		OperatorImage:                        OperatorImageTest,
		InitContainerImage:                   InitContainerImageTest,
		InitContainerImagePullPolicy:         corev1.PullAlways,
		CollectorImage:                       CollectorImageTest,
		CollectorImagePullPolicy:             corev1.PullAlways,
		ConfigurationReloaderImage:           ConfigurationReloaderImageTest,
		ConfigurationReloaderImagePullPolicy: corev1.PullAlways,
		FilelogOffsetSyncImage:               FilelogOffsetSyncImageTest,
		FilelogOffsetSyncImagePullPolicy:     corev1.PullAlways,
	}

	OperatorManagerDeploymentUIDStr = "2f009c75-d69f-4b02-9d9d-fa17e76f5c1d"
	OperatorManagerDeploymentUID    = types.UID(OperatorManagerDeploymentUIDStr)
	OperatorManagerDeploymentName   = "unit-test-dash0-operator-controller"
	OperatorManagerDeployment       = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OperatorNamespace,
			Name:      OperatorManagerDeploymentName,
			UID:       types.UID(OperatorManagerDeploymentUIDStr),
		},
	}

	DanglingEventsTimeoutsTest = util.DanglingEventsTimeouts{
		InitialTimeout: 0 * time.Second,
		Backoff: wait.Backoff{
			Steps:    1,
			Duration: 0 * time.Second,
			Factor:   1,
			Jitter:   0,
		},
	}
)

func NoExport() *dash0common.Export {
	return nil
}

func Dash0ExportWithEndpointAndToken() *dash0common.Export {
	return &dash0common.Export{
		Dash0: &dash0common.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0common.Authorization{
				Token: &AuthorizationTokenTest,
			},
		},
	}
}

func Dash0ExportWithEndpointTokenAndCustomDataset() *dash0common.Export {
	return &dash0common.Export{
		Dash0: &dash0common.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Dataset:  DatasetCustomTest,
			Authorization: dash0common.Authorization{
				Token: &AuthorizationTokenTest,
			},
		},
	}
}

func Dash0ExportWithEndpointAndSecretRef() *dash0common.Export {
	return &dash0common.Export{
		Dash0: &dash0common.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0common.Authorization{
				SecretRef: &SecretRefTest,
			},
		},
	}
}

func GrpcExportTest() *dash0common.Export {
	return &dash0common.Export{
		Grpc: &dash0common.GrpcConfiguration{
			Endpoint: EndpointGrpcTest,
			Headers: []dash0common.Header{{
				Name:  "Key",
				Value: "Value",
			}},
		},
	}
}

func HttpExportTest() *dash0common.Export {
	return &dash0common.Export{
		Http: &dash0common.HttpConfiguration{
			Endpoint: EndpointHttpTest,
			Headers: []dash0common.Header{{
				Name:  "Key",
				Value: "Value",
			}},
			Encoding: dash0common.Proto,
		},
	}
}
