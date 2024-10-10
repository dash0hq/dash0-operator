// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	_ "embed"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/gomega"
)

var (
	//go:embed crds/perses.dev_persesdashboards.yaml
	persesDashboardsCrdYaml         []byte
	PersesDashboardCrdQualifiedName = types.NamespacedName{
		Name: "persesdashboards.perses.dev",
	}

	//go:embed crds/monitoring.coreos.com_prometheusrules.yaml
	prometheusRulesCrdYaml         []byte
	PrometheusRuleCrdQualifiedName = types.NamespacedName{
		Name: "prometheusrules.monitoring.coreos.com",
	}
)

func EnsurePersesDashboardCrdExists(
	ctx context.Context,
	k8sClient client.Client,
) *apiextensionsv1.CustomResourceDefinition {
	return ensureThirdPartyResourceExists(
		ctx,
		k8sClient,
		persesDashboardsCrdYaml,
		PersesDashboardCrdQualifiedName,
	)
}

func EnsurePrometheusRuleCrdExists(
	ctx context.Context,
	k8sClient client.Client,
) *apiextensionsv1.CustomResourceDefinition {
	return ensureThirdPartyResourceExists(
		ctx,
		k8sClient,
		prometheusRulesCrdYaml,
		PrometheusRuleCrdQualifiedName,
	)
}

func ensureThirdPartyResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	crdYaml []byte,
	crdQualifiedName types.NamespacedName,
) *apiextensionsv1.CustomResourceDefinition {
	crdFromYaml := &apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal(crdYaml, crdFromYaml)).To(Succeed())
	crdObject := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		crdQualifiedName,
		&apiextensionsv1.CustomResourceDefinition{},
		crdFromYaml,
	)

	return crdObject.(*apiextensionsv1.CustomResourceDefinition)
}
