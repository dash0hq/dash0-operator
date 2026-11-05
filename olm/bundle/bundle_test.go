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
	It("grants the service account of the collector daemonset the use of the privileged SCC", func() {
		strategy := readInstallStrategy()

		var rules []rbacv1.PolicyRule
		for _, permission := range strategy.ClusterPermissions {
			if permission.ServiceAccountName == otelcolresources.OlmDaemonSetServiceAccountName {
				rules = permission.Rules
			}
		}
		Expect(rules).To(Equal([]rbacv1.PolicyRule{{
			APIGroups:     []string{"security.openshift.io"},
			Resources:     []string{"securitycontextconstraints"},
			ResourceNames: []string{"privileged"},
			Verbs:         []string{"use"},
		}}))
	})

	It("makes the operator use that service account for the collector daemonset", func() {
		strategy := readInstallStrategy()

		Expect(strategy.Deployments).To(HaveLen(1))
		containers := strategy.Deployments[0].Spec.Template.Spec.Containers
		Expect(containers).NotTo(BeEmpty())
		Expect(containers[0].Env).To(ContainElement(corev1.EnvVar{
			Name:  "DASH0_OTEL_COLLECTOR_DAEMONSET_SERVICE_ACCOUNT_NAME",
			Value: otelcolresources.OlmDaemonSetServiceAccountName,
		}))
	})
})

func readInstallStrategy() installStrategy {
	content, err := os.ReadFile(filepath.Join("..", "..", "bundle", "manifests",
		"dash0-operator.clusterserviceversion.yaml"))
	Expect(err).NotTo(HaveOccurred())
	var csv struct {
		Spec struct {
			Install struct {
				Spec installStrategy `json:"spec"`
			} `json:"install"`
		} `json:"spec"`
	}
	Expect(yaml.Unmarshal(content, &csv)).To(Succeed())
	return csv.Spec.Install.Spec
}
