// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	TestNamespaceName           = "test-namespace"
	OperatorNamespace           = "dash0-system"
	OTelCollectorNamePrefixTest = "unit-test"

	CronJobNamePrefix     = "cronjob"
	DaemonSetNamePrefix   = "daemonset"
	DeploymentNamePrefix  = "deployment"
	JobNamePrefix         = "job"
	PodNamePrefix         = "pod"
	ReplicaSetNamePrefix  = "replicaset"
	StatefulSetNamePrefix = "statefulset"

	OperatorImageTest              = "some-registry.com:1234/dash0hq/operator-controller:1.2.3"
	InitContainerImageTest         = "some-registry.com:1234/dash0hq/instrumentation:4.5.6"
	CollectorImageTest             = "some-registry.com:1234/dash0hq/collector:7.8.9"
	ConfigurationReloaderImageTest = "some-registry.com:1234/dash0hq/configuration-reloader:10.11.12"
	FilelogOffsetSynchImageTest    = "some-registry.com:1234/dash0hq/filelog-offset-synch:13.14.15"

	OTelCollectorBaseUrlTest      = "http://$(DASH0_NODE_IP):40318"
	EndpointDash0Test             = "endpoint.dash0.com:4317"
	EndpointDash0TestAlternative  = "endpoint-alternative.dash0.com:4317"
	EndpointDash0TestQuoted       = "\"endpoint.dash0.com:4317\""
	EndpointDash0WithProtocolTest = "https://endpoint.dash0.com:4317"
	EndpointGrpcTest              = "endpoint.backend.com:4317"
	EndpointHttpTest              = "https://endpoint.backend.com:4318"

	ApiEndpointTest = "https://api.dash0.com"
	DatasetTest     = "test-dataset"
)

var (
	AuthorizationTokenTest            = "authorization-token"
	AuthorizationTokenTestAlternative = "authorization-token-test"
	SecretRefTest                     = dash0v1alpha1.SecretRef{
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
		FilelogOffsetSynchImage:              FilelogOffsetSynchImageTest,
		FilelogOffsetSynchImagePullPolicy:    corev1.PullAlways,
	}

	DeploymentSelfReference = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OperatorNamespace,
			Name:      "unit-test-dash0-operator-controller",
			UID:       "2f009c75-d69f-4b02-9d9d-fa17e76f5c1d",
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

func Dash0ExportWithEndpointAndToken() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0v1alpha1.Authorization{
				Token: &AuthorizationTokenTest,
			},
		},
	}
}

func Dash0ExportWithEndpointTokenAndCustomDataset() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Dataset:  DatasetTest,
			Authorization: dash0v1alpha1.Authorization{
				Token: &AuthorizationTokenTest,
			},
		},
	}
}

func Dash0ExportWithEndpointAndTokenAndApiEndpoint() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0v1alpha1.Authorization{
				Token: &AuthorizationTokenTest,
			},
			ApiEndpoint: ApiEndpointTest,
		},
	}
}

func Dash0ExportWithEndpointAndSecretRef() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0v1alpha1.Authorization{
				SecretRef: &SecretRefTest,
			},
		},
	}
}

func Dash0ExportWithEndpointAndSecretRefAndApiEndpoint() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Dash0: &dash0v1alpha1.Dash0Configuration{
			Endpoint: EndpointDash0Test,
			Authorization: dash0v1alpha1.Authorization{
				SecretRef: &SecretRefTest,
			},
			ApiEndpoint: ApiEndpointTest,
		},
	}
}

func ExportToPrt(export dash0v1alpha1.Export) *dash0v1alpha1.Export {
	return &export
}

func GrpcExportTest() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Grpc: &dash0v1alpha1.GrpcConfiguration{
			Endpoint: EndpointGrpcTest,
			Headers: []dash0v1alpha1.Header{{
				Name:  "Key",
				Value: "Value",
			}},
		},
	}
}

func HttpExportTest() dash0v1alpha1.Export {
	return dash0v1alpha1.Export{
		Http: &dash0v1alpha1.HttpConfiguration{
			Endpoint: EndpointHttpTest,
			Headers: []dash0v1alpha1.Header{{
				Name:  "Key",
				Value: "Value",
			}},
			Encoding: dash0v1alpha1.Proto,
		},
	}
}
