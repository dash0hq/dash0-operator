// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package swresources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

const (
	syntheticsWorker           = "dash0-synthetics-worker"
	syntheticsWorkerNameSuffix = "synthetics-worker"

	containerName = "synthetics-worker"

	// authTokenEnvVarName is the environment variable through which the synthetics-worker workload receives the
	// Dash0 authorization token (either a literal token value or resolved from a Kubernetes secret reference). It is
	// never read directly by the synthetics-worker binary; headersEnvVarName references it via a Kubernetes
	// "$(VAR_NAME)" substitution, since Kubernetes cannot template a secret's value into a larger string via
	// valueFrom.
	authTokenEnvVarName = "DASH0_SYNTHETICS_WORKER_AUTH_TOKEN"

	// headersEnvVarName carries the comma-separated headers the synthetics-worker attaches to its outbound stream to
	// the Dash0 backend, which must include an "authorization=Bearer <token>" entry.
	headersEnvVarName = "SYNTHETICS_HEADERS"

	// probePort is the fixed port the synthetics-worker serves its gRPC health checks on, see
	// components/synthetics-worker/internal/health/health.go in the main Dash0 repository.
	probePort = 8011

	livenessServiceName  = "liveness"
	readinessServiceName = "readiness"

	// label values
	appKubernetesIoNameValue      = syntheticsWorker
	appKubernetesIoInstanceValue  = "dash0-operator"
	appKubernetesIoManagedByValue = "dash0-operator"

	defaultReplicas int32 = 1

	defaultUser  int64 = 65532
	defaultGroup int64 = 0
)

var deploymentMatchLabels = map[string]string{
	util.AppKubernetesIoNameLabel:     appKubernetesIoNameValue,
	util.AppKubernetesIoInstanceLabel: appKubernetesIoInstanceValue,
}

// This type just exists to ensure all created objects go through addCommonMetadata.
type clientObject struct {
	object client.Object
}

// selfMonitoringInput carries everything the self-monitoring of the synthetics-worker workload is derived from. In
// contrast to util.SyntheticsWorkerConfig, which is fixed when the operator manager starts, it comes from the
// Dash0OperatorConfiguration resource and is therefore read again for every reconciliation.
type selfMonitoringInput struct {
	configuration selfmonitoringapiaccess.SelfMonitoringConfiguration
	clusterName   string
}

func assembleDesiredState(
	config *util.SyntheticsWorkerConfig,
	spec dash0v1alpha1.SyntheticsWorker,
	authTokenEnvVar *corev1.EnvVar,
	selfMonitoring selfMonitoringInput,
) ([]clientObject, error) {
	deployment, err := assembleDeployment(config, spec, authTokenEnvVar, selfMonitoring)
	if err != nil {
		return nil, err
	}
	desiredState := make([]clientObject, 0, 2)
	desiredState = append(desiredState, addCommonMetadata(assembleServiceAccount(config)))
	desiredState = append(desiredState, addCommonMetadata(deployment))
	return desiredState, nil
}

func assembleServiceAccount(c *util.SyntheticsWorkerConfig) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: util.K8sApiVersionCoreV1,
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName(c.NamePrefix),
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
		},
	}
}

func assembleDeployment(
	c *util.SyntheticsWorkerConfig,
	spec dash0v1alpha1.SyntheticsWorker,
	authTokenEnvVar *corev1.EnvVar,
	selfMonitoring selfMonitoringInput,
) (*appsv1.Deployment, error) {
	deploymentName := DeploymentName(c.NamePrefix)

	replicas := ptr.Deref(spec.Replicas, defaultReplicas)

	logLevel := "info"
	if c.DevelopmentMode {
		logLevel = "debug"
	}

	container := corev1.Container{
		Name:  containerName,
		Image: c.Images.SyntheticsWorkerImage,
		Env: []corev1.EnvVar{
			{
				// The address of the Dash0 backend service the synthetics-worker workload connects to.
				Name:  "SYNTHETICS_ADDRESS",
				Value: c.ServerAddress,
			},
			{
				// The private location this cluster's synthetics-worker executes checks for.
				Name:  "SYNTHETICS_LOCATIONID",
				Value: spec.LocationID,
			},
			{
				// Fixed; matches the port the health probes below target.
				Name:  "LISTENADDRESS",
				Value: fmt.Sprintf(":%d", probePort),
			},
			{
				Name:  "LOGLEVEL",
				Value: logLevel,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			RunAsNonRoot:             ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    probePort,
					Service: ptr.To(livenessServiceName),
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				GRPC: &corev1.GRPCAction{
					Port:    probePort,
					Service: ptr.To(readinessServiceName),
				},
			},
		},
	}

	if spec.Resources != nil {
		container.Resources = *spec.Resources
	}

	if authTokenEnvVar != nil {
		// A missing authorization token is tolerated here so the desired state can still be assembled for
		// DeleteResources (where the token is irrelevant). Kubernetes expands "$(VAR_NAME)" references in a
		// container's env "value" field against previously-defined env vars in the same list (including ones sourced
		// via valueFrom), which is how a secret-backed token ends up in SYNTHETICS_HEADERS without the operator ever
		// reading the secret itself; authTokenEnvVar must therefore be appended before SYNTHETICS_HEADERS.
		container.Env = append(container.Env, *authTokenEnvVar)
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  headersEnvVarName,
			Value: fmt.Sprintf("authorization=Bearer $(%s)", authTokenEnvVarName),
		})
	}

	if c.Insecure {
		container.Env = append(container.Env, corev1.EnvVar{
			// Disables TLS for the connection to the Dash0 backend; only intended for local development.
			Name:  "SYNTHETICS_INSECURE",
			Value: "true",
		})
	}

	if c.Images.SyntheticsWorkerImagePullPolicy != "" {
		container.ImagePullPolicy = c.Images.SyntheticsWorkerImagePullPolicy
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: ServiceAccountName(c.NamePrefix),
		Containers: []corev1.Container{
			container,
		},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			RunAsUser:  util.RunAsID(c.IsOpenShift, defaultUser),
			RunAsGroup: util.RunAsID(c.IsOpenShift, defaultGroup),
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: c.OperatorNamespace,
			Labels:    labels(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentMatchLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels(),
				},
				Spec: podSpec,
			},
		},
	}

	if selfMonitoring.configuration.SelfMonitoringEnabled {
		if err := selfmonitoringapiaccess.EnableSelfMonitoringInDeployment(
			deployment,
			selfMonitoring.configuration,
			c.Images.GetOperatorVersion(),
			c.DevelopmentMode,
			"",
		); err != nil {
			return nil, err
		}
	}

	return deployment, nil
}

// ---utils---

func ServiceAccountName(namePrefix string) string {
	return resources.RenderName(namePrefix, syntheticsWorkerNameSuffix, "sa")
}

// DeploymentName returns the name of the synthetics-worker deployment, which is "<namePrefix>-synthetics-worker".
func DeploymentName(namePrefix string) string {
	return resources.RenderName(namePrefix, syntheticsWorkerNameSuffix)
}

func addCommonMetadata(object client.Object) clientObject {
	// For clusters managed by ArgoCD, we need to prevent ArgoCD to sync or prune resources that have no owner
	// reference. See the identical comment in a0cresources.addCommonMetadata for the full rationale.
	if object.GetAnnotations() == nil {
		object.SetAnnotations(map[string]string{})
	}
	object.GetAnnotations()["argocd.argoproj.io/sync-options"] = "Prune=false"
	object.GetAnnotations()["argocd.argoproj.io/compare-options"] = "IgnoreExtraneous"
	return clientObject{
		object: object,
	}
}

func labels() map[string]string {
	return map[string]string{
		util.AppKubernetesIoNameLabel:      appKubernetesIoNameValue,
		util.AppKubernetesIoInstanceLabel:  appKubernetesIoInstanceValue,
		util.AppKubernetesIoManagedByLabel: appKubernetesIoManagedByValue,
	}
}
