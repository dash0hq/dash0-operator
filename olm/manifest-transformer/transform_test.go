// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testVersion = "1.2.3"

	deploymentManifest = `
# Source: dash0-operator/templates/operator/deployment-and-webhooks.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dash0-operator-controller
  namespace: default
  labels:
    app.kubernetes.io/name: dash0-operator
    app.kubernetes.io/component: controller
    app.kubernetes.io/version: "0.0.0"
    app.kubernetes.io/managed-by: Helm
    helm.sh/chart: dash0-operator-0.0.0
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: dash0-operator
      app.kubernetes.io/component: controller
  template:
    metadata:
      labels:
        app.kubernetes.io/name: dash0-operator
        app.kubernetes.io/component: controller
        app.kubernetes.io/version: "0.0.0"
        app.kubernetes.io/managed-by: Helm
        helm.sh/chart: dash0-operator-0.0.0
    spec:
      containers:
      - name: manager
        image: "operator-controller:latest"
        command:
        - /manager
        args:
        - --health-probe-bind-address=:8081
        - --metrics-bind-address=127.0.0.1:8080
        - --leader-elect
        - --dash0-telemetry-collection-enabled=true
        - --dash0-feature-signal-control-enabled=true
        - --dash0-otel-collector-otlp-grpc-host-port=40317
        - --dash0-log-level=info
        env:
        - name: DASH0_OPERATOR_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: DASH0_WEBHOOK_SERVICE_NAME
          value: dash0-operator-webhook-service
        - name: DASH0_OPERATOR_IMAGE
          value: "operator-controller:latest"
        - name: DASH0_COLLECTOR_IMAGE
          value: "collector:latest"
        - name: DASH0_COLLECTOR_IMAGE_PULL_POLICY
          value: Always
        - name: DASH0_AGENT0_CONNECTOR_ENABLED
          value: "true"
        - name: DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS
          value: "olm-bundle-placeholder"
        - name: DASH0_AGENT0_CONNECTOR_SECRET_REF_NAME
          value: "olm-bundle-placeholder"
        - name: DASH0_AGENT0_CONNECTOR_SECRET_REF_KEY
          value: "olm-bundle-placeholder"
        - name: DASH0_AGENT0_CONNECTOR_INSECURE
          value: "true"
        ports:
        - containerPort: 9443
          name: webhook-server
          protocol: TCP
        volumeMounts:
        - name: certificates
          mountPath: /tmp/k8s-webhook-server/serving-certs
          readOnly: true
        - name: config-volume
          mountPath: /etc/config
          readOnly: true
      serviceAccountName: dash0-operator-controller
      volumes:
      - name: certificates
        secret:
          defaultMode: 420
          secretName: "olm-bundle-placeholder"
      - name: config-volume
        configMap:
          name: dash0-operator-extra-config
`

	webhookManifest = `
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: dash0-operator-injector
  labels:
    app.kubernetes.io/name: dash0-operator
    app.kubernetes.io/version: "0.0.0"
    helm.sh/chart: dash0-operator-0.0.0
  annotations:
    dash0.com/olm-bundle-placeholder: remove
webhooks:
- name: inject.dash0.com
  admissionReviewVersions:
  - v1
  namespaceSelector:
    matchExpressions:
    - key: kubernetes.io/metadata.name
      operator: NotIn
      values:
      - kube-system
  clientConfig:
    service:
      name: dash0-operator-webhook-service
      namespace: default
      path: /workloads/inject
  sideEffects: None
`

	helmHookManifest = `
apiVersion: batch/v1
kind: Job
metadata:
  name: dash0-operator-pre-delete
  annotations:
    helm.sh/hook: pre-delete
spec:
  template:
    spec:
      containers:
      - name: uninstrument
        image: operator-controller:latest
      restartPolicy: Never
`

	serviceManifest = `
apiVersion: v1
kind: Service
metadata:
  name: dash0-operator-webhook-service
  annotations:
    example.com/kept: "true"
    dash0.com/olm-bundle-placeholder: remove
spec:
  ports:
  - port: 443
    targetPort: 9443
  selector:
    app.kubernetes.io/name: dash0-operator
    app.kubernetes.io/component: controller
`

	crdHeader = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: dash0monitorings.operator.dash0.com
spec:
  group: operator.dash0.com
  names:
    kind: Dash0Monitoring
    plural: dash0monitorings
  scope: Namespaced
`
	crdConversion = `  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        service:
          name: dash0-operator-webhook-service
          namespace: default
          path: /convert
      conversionReviewVersions:
      - v1
`
	crdVersions = `  versions:
  - name: v1alpha1
    served: true
    storage: false
    schema:
      openAPIV3Schema:
        type: object
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`
	crdManifest                  = crdHeader + crdConversion + crdVersions
	crdManifestWithoutConversion = crdHeader + crdVersions

	csvManifest = `
apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: dash0-operator.v0.0.0
  annotations:
    capabilities: Seamless Upgrades
spec:
  displayName: Dash0 Operator
`
)

var _ = Describe("the OLM manifest transformer", func() {
	It("drops Helm hooks and empty documents and keeps all other documents in order", func() {
		out, log := transformSuccessfully(helmHookManifest, "# Source: empty.yaml\n", webhookManifest, deploymentManifest)

		docs := parseOutput(out)
		Expect(docs).To(HaveLen(2))
		Expect(docs[0].GetKind()).To(Equal("MutatingWebhookConfiguration"))
		Expect(docs[1].GetKind()).To(Equal("Deployment"))
		Expect(log).To(ContainSubstring("dropped the Helm hook Job dash0-operator-pre-delete"))
	})

	It("warns about webhook namespace selectors, which OLM does not apply", func() {
		_, log := transformSuccessfully(webhookManifest, deploymentManifest)

		Expect(log).To(ContainSubstring(
			"warning: OLM does not apply the namespaceSelector of the webhook inject.dash0.com in " +
				"MutatingWebhookConfiguration dash0-operator-injector"))
	})

	DescribeTable("rejects kinds that are not expected in the bundle",
		func(manifest string) {
			err := runTransform(manifest, deploymentManifest)
			Expect(err).To(MatchError(ContainSubstring("is not expected in the bundle")))
		},
		Entry("the Secret with the webhook certificate", `
apiVersion: v1
kind: Secret
metadata:
  name: dash0-operator-certificates
`),
		Entry("the OpenShift SecurityContextConstraints", `
apiVersion: security.openshift.io/v1
kind: SecurityContextConstraints
metadata:
  name: dash0-operator-scc
`),
	)

	It("fails without a Deployment", func() {
		Expect(runTransform(webhookManifest)).To(MatchError("expected exactly one Deployment, found 0"))
	})

	It("fails with more than one Deployment", func() {
		Expect(runTransform(deploymentManifest, deploymentManifest)).To(
			MatchError("expected exactly one Deployment, found 2"))
	})

	It("sets the containerImage annotation of the ClusterServiceVersion to the manager image", func() {
		out, log := transformSuccessfully(csvManifest, deploymentManifest)

		csv := parseOutput(out)[0]
		Expect(csv.GetAnnotations()).To(Equal(map[string]string{
			"capabilities":   "Seamless Upgrades",
			"containerImage": "operator-controller:latest",
		}))
		Expect(log).To(ContainSubstring(
			"set the annotation containerImage of the ClusterServiceVersion dash0-operator.v0.0.0 to " +
				"operator-controller:latest"))
	})

	It("fails with more than one ClusterServiceVersion", func() {
		Expect(runTransform(csvManifest, csvManifest, deploymentManifest)).To(
			MatchError("expected at most one ClusterServiceVersion, found 2"))
	})

	It("fails if the manager container has no image", func() {
		err := runTransform(deploymentWith("        image: \"operator-controller:latest\"\n", ""))
		Expect(err).To(MatchError(ContainSubstring("the container manager has no image")))
	})

	It("removes the placeholder annotation and keeps other annotations", func() {
		out, _ := transformSuccessfully(webhookManifest, serviceManifest, deploymentManifest)

		docs := parseOutput(out)
		Expect(docs[0].GetAnnotations()).To(BeEmpty())
		Expect(docs[1].GetAnnotations()).To(Equal(map[string]string{"example.com/kept": "true"}))
	})

	It("fails if a placeholder value is left in the output", func() {
		err := runTransform(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: dash0-operator-extra-config
data:
  extra.yaml: "secretName: olm-bundle-placeholder"
`, deploymentManifest)
		Expect(err).To(MatchError(ContainSubstring(
			"ConfigMap dash0-operator-extra-config still contains the placeholder")))
	})

	It("removes the Helm labels and sets the version label", func() {
		out, _ := transformSuccessfully(webhookManifest, deploymentManifest)

		docs := parseOutput(out)
		expectedLabels := map[string]string{
			"app.kubernetes.io/name":    "dash0-operator",
			"app.kubernetes.io/version": testVersion,
		}
		Expect(docs[0].GetLabels()).To(Equal(expectedLabels))
		deployment := toDeployment(docs[1])
		Expect(deployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/version", testVersion))
		Expect(deployment.Labels).NotTo(HaveKey("helm.sh/chart"))
		Expect(deployment.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))
		Expect(deployment.Spec.Template.Labels).To(Equal(map[string]string{
			"app.kubernetes.io/name":      "dash0-operator",
			"app.kubernetes.io/component": "controller",
			"app.kubernetes.io/version":   testVersion,
		}))
		Expect(deployment.Spec.Selector.MatchLabels).To(Equal(map[string]string{
			"app.kubernetes.io/name":      "dash0-operator",
			"app.kubernetes.io/component": "controller",
		}))
	})

	Describe("custom resource definitions", func() {
		It("serves only the storage version of a CustomResourceDefinition with a conversion webhook", func() {
			out, log := transformSuccessfully(crdManifest, deploymentManifest)

			crd := parseOutput(out)[0]
			versions, _, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
			Expect(err).NotTo(HaveOccurred())
			Expect(versions).To(HaveLen(1))
			Expect(versions[0]).To(HaveKeyWithValue("name", "v1beta1"))
			Expect(versions[0]).To(HaveKeyWithValue("served", true))
			Expect(versions[0]).To(HaveKeyWithValue("storage", true))
			Expect(versions[0]).To(HaveKey("schema"))
			Expect(crd.Object["spec"]).NotTo(HaveKey("conversion"))
			Expect(log).To(ContainSubstring(
				"removed the conversion webhook and all versions but the storage version v1beta1 from the " +
					"CustomResourceDefinition dash0monitorings.operator.dash0.com"))
		})

		It("keeps all versions of a CustomResourceDefinition without a conversion webhook", func() {
			out, log := transformSuccessfully(crdManifestWithoutConversion, deploymentManifest)

			versions, _, err := unstructured.NestedSlice(parseOutput(out)[0].Object, "spec", "versions")
			Expect(err).NotTo(HaveOccurred())
			Expect(versions).To(HaveLen(2))
			Expect(log).NotTo(ContainSubstring("CustomResourceDefinition"))
		})

		DescribeTable("rejects a CRD with a conversion webhook that cannot serve only its storage version",
			func(old string, replacement string, expectedError string) {
				Expect(crdManifest).To(ContainSubstring(old))
				err := runTransform(strings.Replace(crdManifest, old, replacement, 1), deploymentManifest)
				Expect(err).To(MatchError(
					"the CustomResourceDefinition dash0monitorings.operator.dash0.com: " + expectedError))
			},
			Entry("no storage version", "storage: true", "storage: false", "there is no storage version"),
			Entry("a storage version that is not served",
				"served: true\n    storage: true", "served: false\n    storage: true",
				"the storage version v1beta1 is not served"),
			Entry("a version newer than the storage version", "- name: v1alpha1", "- name: v1",
				"the version v1 is newer than the storage version v1beta1 and would not be served"),
		)
	})

	Describe("the manager container", func() {
		It("keeps the fixed args and converts all other args to environment variables", func() {
			container, log := transformManagerContainer(deploymentManifest)

			Expect(container.Args).To(Equal([]string{
				"--health-probe-bind-address=:8081",
				"--metrics-bind-address=127.0.0.1:8080",
				"--leader-elect",
			}))
			Expect(container.Env).To(ContainElements(
				corev1.EnvVar{Name: "DASH0_TELEMETRY_COLLECTION_ENABLED", Value: "true"},
				corev1.EnvVar{Name: "DASH0_FEATURE_SIGNAL_CONTROL_ENABLED", Value: "true"},
				corev1.EnvVar{Name: "DASH0_OTEL_COLLECTOR_OTLP_GRPC_HOST_PORT", Value: "40317"},
				corev1.EnvVar{Name: "DASH0_LOG_LEVEL", Value: "info"},
			))
			Expect(log).To(ContainSubstring(
				"converted the argument --dash0-log-level to the environment variable DASH0_LOG_LEVEL"))
		})

		It("converts a bool flag without a value to true", func() {
			container, _ := transformManagerContainer(deploymentWith(
				"- --dash0-telemetry-collection-enabled=true", "- --dash0-telemetry-collection-enabled"))

			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_TELEMETRY_COLLECTION_ENABLED", Value: "true"}))
		})

		It("converts a bool flag set to false", func() {
			container, _ := transformManagerContainer(deploymentWith(
				"- --dash0-log-level=info", "- --dash0-gke-autopilot=false\n        - --dash0-log-level=info"))

			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "DASH0_GKE_AUTOPILOT", Value: "false"}))
		})

		It("fails without the manager container", func() {
			err := runTransform(deploymentWith("- name: manager", "- name: other"))
			Expect(err).To(MatchError(ContainSubstring("there is no container named manager")))
		})

		DescribeTable("rejects args it cannot convert",
			func(arg string, expectedError string) {
				err := runTransform(deploymentWith("- --dash0-log-level=info", "- "+arg))
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
			},
			Entry("an unknown flag", "--no-such-flag=1", "is not a command-line flag of the operator manager"),
			Entry("an invalid value", "--dash0-otel-collector-otlp-http-host-port=abc", "has an invalid value"),
			Entry("a non-bool flag without a value", "--dash0-log-level", "has no value"),
			Entry("a positional arg", "info", "only --name and --name=value are supported"),
			Entry("an empty value", "--dash0-log-level=", "has an empty value"),
			Entry("an operator configuration arg",
				"--operator-configuration-endpoint=ingress.example.com:4317", "is not supported in the bundle"),
		)

		It("fails if an arg would override an existing environment variable", func() {
			err := runTransform(deploymentWith(
				"        - name: DASH0_OPERATOR_IMAGE\n",
				"        - name: DASH0_LOG_LEVEL\n          value: debug\n        - name: DASH0_OPERATOR_IMAGE\n"))
			Expect(err).To(MatchError(ContainSubstring("the environment variable DASH0_LOG_LEVEL is already set")))
		})

		It("sets the webhook service name to the service OLM creates", func() {
			container, _ := transformManagerContainer(deploymentManifest)

			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_WEBHOOK_SERVICE_NAME", Value: "dash0-operator-controller-service"}))
		})

		It("fails without the webhook service name", func() {
			err := runTransform(deploymentWith(
				"        - name: DASH0_WEBHOOK_SERVICE_NAME\n          value: dash0-operator-webhook-service\n", ""))
			Expect(err).To(MatchError(ContainSubstring(
				"the environment variable DASH0_WEBHOOK_SERVICE_NAME is missing")))
		})

		It("disables the agent0-connector and removes its placeholder settings", func() {
			container, _ := transformManagerContainer(deploymentManifest)

			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_AGENT0_CONNECTOR_ENABLED", Value: "false"}))
			for _, envVar := range container.Env {
				Expect(agent0ConnectorSettingEnvVarNames).NotTo(ContainElement(envVar.Name))
			}
		})

		It("fails if the chart was rendered without the agent0-connector", func() {
			err := runTransform(deploymentWith(
				"        - name: DASH0_AGENT0_CONNECTOR_ENABLED\n          value: \"true\"\n", ""))
			Expect(err).To(MatchError(ContainSubstring("render the chart with operator.agent0Connector.enabled=true")))
		})

		It("removes the webhook certificate volume and keeps the other volumes", func() {
			docs := parseOutput(mustTransform(deploymentManifest))
			podSpec := toDeployment(docs[0]).Spec.Template.Spec

			Expect(podSpec.Volumes).To(HaveLen(1))
			Expect(podSpec.Volumes[0].Name).To(Equal("config-volume"))
			Expect(podSpec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(podSpec.Containers[0].VolumeMounts[0].Name).To(Equal("config-volume"))
		})

		It("fails if no volume is mounted at the path of the webhook certificate", func() {
			err := runTransform(deploymentWith(
				"mountPath: /tmp/k8s-webhook-server/serving-certs", "mountPath: /certs"))
			Expect(err).To(MatchError(ContainSubstring("no volume is mounted at /tmp/k8s-webhook-server/serving-certs")))
		})

		It("fails if another container mounts the webhook certificate volume", func() {
			err := runTransform(deploymentWith(
				"      serviceAccountName: dash0-operator-controller\n",
				"      initContainers:\n"+
					"      - name: init\n"+
					"        image: busybox\n"+
					"        volumeMounts:\n"+
					"        - name: certificates\n"+
					"          mountPath: /certs\n"+
					"      serviceAccountName: dash0-operator-controller\n"))
			Expect(err).To(MatchError(ContainSubstring("the container init also mounts the volume certificates")))
		})

		It("fails if a related image variable is already set", func() {
			err := runTransform(deploymentWith(
				"        - name: DASH0_OPERATOR_IMAGE\n",
				"        - name: RELATED_IMAGE_COLLECTOR\n          value: collector:1.0.0\n"+
					"        - name: DASH0_OPERATOR_IMAGE\n"))
			Expect(err).To(MatchError(ContainSubstring("the environment variable RELATED_IMAGE_COLLECTOR is already set")))
		})

		It("fails if an image variable has no literal value", func() {
			err := runTransform(deploymentWith(
				"          value: \"collector:latest\"\n",
				"          valueFrom:\n            configMapKeyRef:\n              name: images\n              key: collector\n"))
			Expect(err).To(MatchError(ContainSubstring(
				"the environment variable DASH0_COLLECTOR_IMAGE has no literal value")))
		})

		It("adds a related image variable for every image variable", func() {
			container, _ := transformManagerContainer(deploymentManifest)

			Expect(container.Env).To(ContainElements(
				corev1.EnvVar{Name: "RELATED_IMAGE_OPERATOR", Value: "operator-controller:latest"},
				corev1.EnvVar{Name: "RELATED_IMAGE_COLLECTOR", Value: "collector:latest"},
			))
			for _, envVar := range container.Env {
				Expect(envVar.Name).NotTo(Equal("RELATED_IMAGE_COLLECTOR_IMAGE_PULL_POLICY"))
			}
		})

		It("keeps the environment variables that do not need a change", func() {
			container, _ := transformManagerContainer(deploymentManifest)

			Expect(container.Env).To(ContainElement(corev1.EnvVar{
				Name: "DASH0_OPERATOR_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			}))
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "DASH0_COLLECTOR_IMAGE_PULL_POLICY", Value: "Always"}))
		})
	})
})

func runTransform(manifests ...string) error {
	var out, log bytes.Buffer
	return transform(strings.NewReader(joinManifests(manifests)), &out, &log, testVersion)
}

func transformSuccessfully(manifests ...string) (string, string) {
	var out, log bytes.Buffer
	Expect(transform(strings.NewReader(joinManifests(manifests)), &out, &log, testVersion)).To(Succeed())
	return out.String(), log.String()
}

func mustTransform(manifests ...string) string {
	out, _ := transformSuccessfully(manifests...)
	return out
}

func transformManagerContainer(deployment string) (corev1.Container, string) {
	out, log := transformSuccessfully(deployment)
	docs := parseOutput(out)
	Expect(docs).To(HaveLen(1))
	containers := toDeployment(docs[0]).Spec.Template.Spec.Containers
	Expect(containers).To(HaveLen(1))
	return containers[0], log
}

func joinManifests(manifests []string) string {
	return "---\n" + strings.Join(manifests, "\n---\n")
}

func parseOutput(out string) []*unstructured.Unstructured {
	docs, err := readDocuments(strings.NewReader(out))
	Expect(err).NotTo(HaveOccurred())
	return docs
}

func toDeployment(doc *unstructured.Unstructured) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(doc.Object, deployment)).To(Succeed())
	return deployment
}

func deploymentWith(old string, replacement string) string {
	Expect(deploymentManifest).To(ContainSubstring(old))
	return strings.Replace(deploymentManifest, old, replacement, 1)
}
