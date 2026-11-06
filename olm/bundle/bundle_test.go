// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"os"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/profiling/prresources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// installStrategy is the part of spec.install.spec of the ClusterServiceVersion that these tests read.
type installStrategy struct {
	ClusterPermissions []struct {
		ServiceAccountName string              `json:"serviceAccountName"`
		Rules              []rbacv1.PolicyRule `json:"rules"`
	} `json:"clusterPermissions"`
	Deployments []struct {
		Spec appsv1.DeploymentSpec `json:"spec"`
	} `json:"deployments"`
}

// make olm-bundle-check keeps the committed bundle in sync with its inputs, so these tests cover the generation via
// olm/bin/generate-bundle.sh and operator-sdk without running them.
var _ = Describe("The committed OLM bundle", func() {
	DescribeTable("grants a service account the use of the privileged SCC and makes the operator use it",
		func(serviceAccountName string, envVarName string) {
			strategy := readCsvSpec().Install.Spec

			var rules []rbacv1.PolicyRule
			for _, permission := range strategy.ClusterPermissions {
				if permission.ServiceAccountName == serviceAccountName {
					rules = permission.Rules
				}
			}
			Expect(rules).To(Equal([]rbacv1.PolicyRule{{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{"privileged"},
				Verbs:         []string{"use"},
			}}))
			Expect(managerContainer(strategy).Env).To(ContainElement(
				corev1.EnvVar{Name: envVarName, Value: serviceAccountName}))
		},
		Entry("the collector daemonset",
			otelcolresources.OlmDaemonSetServiceAccountName, "DASH0_OTEL_COLLECTOR_DAEMONSET_SERVICE_ACCOUNT_NAME"),
		Entry("the eBPF profiler",
			prresources.OlmEbpfProfilerServiceAccountName, "DASH0_EBPF_PROFILER_SERVICE_ACCOUNT_NAME"),
	)

	It("carries the images of the eBPF profiler with the profiler switched off", func() {
		spec := readCsvSpec()

		env := managerContainer(spec.Install.Spec).Env
		Expect(env).To(ContainElement(corev1.EnvVar{Name: "DASH0_EBPF_PROFILER_ENABLED", Value: "false"}))
		relatedImages := make([]string, 0, len(spec.RelatedImages))
		for _, relatedImage := range spec.RelatedImages {
			relatedImages = append(relatedImages, relatedImage.Image)
		}
		for _, image := range []struct{ envVarName, repository string }{
			{"DASH0_EBPF_PROFILER_IMAGE", "ghcr.io/parca-dev/parca-agent:"},
			{"DASH0_DEBUGINFO_PROXY_IMAGE", "ghcr.io/dash0hq/debuginfo-proxy:"},
		} {
			Expect(env).To(ContainElement(SatisfyAll(
				HaveField("Name", image.envVarName), HaveField("Value", HavePrefix(image.repository)))))
			Expect(relatedImages).To(ContainElement(HavePrefix(image.repository)))
		}
	})
})

// csvSpec is the part of the spec of the ClusterServiceVersion that these tests read.
type csvSpec struct {
	Install struct {
		Spec installStrategy `json:"spec"`
	} `json:"install"`
	RelatedImages []struct {
		Image string `json:"image"`
	} `json:"relatedImages"`
}

func readCsvSpec() csvSpec {
	content, err := os.ReadFile(filepath.Join("..", "..", "bundle", "manifests",
		"dash0-operator.clusterserviceversion.yaml"))
	Expect(err).NotTo(HaveOccurred())
	var csv struct {
		Spec csvSpec `json:"spec"`
	}
	Expect(yaml.Unmarshal(content, &csv)).To(Succeed())
	return csv.Spec
}

func managerContainer(strategy installStrategy) corev1.Container {
	Expect(strategy.Deployments).To(HaveLen(1))
	containers := strategy.Deployments[0].Spec.Template.Spec.Containers
	Expect(containers).NotTo(BeEmpty())
	return containers[0]
}
