// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	kubeversion "k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/internal/startup"
)

const (
	// placeholder is the value olm/values-olm.yaml uses for settings the chart requires but the bundle must not contain.
	placeholder           = "olm-bundle-placeholder"
	placeholderAnnotation = "dash0.com/" + placeholder
	helmHookAnnotation    = "helm.sh/hook"
	versionLabel          = "app.kubernetes.io/version"

	kindCustomResourceDefinition = "CustomResourceDefinition"
	kindDeployment               = "Deployment"
	managerContainerName         = "manager"

	// OLM mounts the webhook serving certificate at the default path of the controller-runtime webhook server.
	webhookServingCertsPath = "/tmp/k8s-webhook-server/serving-certs"
	// OLM creates the webhook service as <deployment name>-service.
	olmWebhookServiceNameSuffix = "-service"

	// The env var names mirror the constants in internal/startup/operator_manager_startup.go.
	webhookServiceNameEnvVarName     = "DASH0_WEBHOOK_SERVICE_NAME"
	agent0ConnectorEnabledEnvVarName = "DASH0_AGENT0_CONNECTOR_ENABLED"
	relatedImageEnvVarPrefix         = "RELATED_IMAGE_"
)

var (
	// supportedKinds lists the kinds the chart renders for the bundle; anything else (for example the OpenShift
	// SecurityContextConstraints or the Secret with the generated webhook certificate) means that olm/values-olm.yaml
	// no longer matches the chart.
	supportedKinds = []string{
		"ClusterRole",
		"ClusterRoleBinding",
		"ConfigMap",
		kindCustomResourceDefinition,
		kindDeployment,
		"MutatingWebhookConfiguration",
		"Role",
		"RoleBinding",
		"Service",
		"ServiceAccount",
		"ValidatingWebhookConfiguration",
	}

	removedLabels = []string{"helm.sh/chart", "app.kubernetes.io/managed-by"}

	// fixedArgs stay container args, a Subscription must not change them: the bind addresses have to match the probes of
	// the Deployment, and leader election has to stay enabled. All other args are converted to environment variables,
	// which a Subscription can override via spec.config.env.
	fixedArgs = []string{"health-probe-bind-address", "metrics-bind-address", "leader-elect"}

	// operatorConfigurationArgPrefix marks the args for the operator configuration resource the operator creates for
	// Helm installs. Under OLM, users create the Dash0OperatorConfiguration resource themselves.
	operatorConfigurationArgPrefix = "operator-configuration-"

	// agent0ConnectorSettingEnvVarNames are all agent0-connector settings the chart can render; olm/values-olm.yaml fills
	// the mandatory ones with placeholders. They are set via the Subscription together with
	// DASH0_AGENT0_CONNECTOR_ENABLED=true. A mandatory setting missing here fails the placeholder check, an optional one
	// keeps its chart value in the bundle.
	agent0ConnectorSettingEnvVarNames = []string{
		"DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS",
		"DASH0_AGENT0_CONNECTOR_TOKEN",
		"DASH0_AGENT0_CONNECTOR_SECRET_REF_NAME",
		"DASH0_AGENT0_CONNECTOR_SECRET_REF_KEY",
		"DASH0_AGENT0_CONNECTOR_INSECURE",
	}

	imageEnvVarNamePattern = regexp.MustCompile(`^DASH0_([A-Z0-9_]+)_IMAGE$`)
)

// transform reads the multi-document output of helm template from in and writes the manifests for operator-sdk generate
// bundle to out. Every change is reported on log. It fails on any input it does not expect, so that a change to the
// chart cannot silently produce a broken bundle.
func transform(in io.Reader, out io.Writer, log io.Writer, version string) error {
	docs, err := readDocuments(in)
	if err != nil {
		return err
	}
	kept := make([]*unstructured.Unstructured, 0, len(docs))
	deployments := 0
	for _, doc := range docs {
		if _, isHook := doc.GetAnnotations()[helmHookAnnotation]; isHook {
			fmt.Fprintf(log, "dropped the Helm hook %s %s\n", doc.GetKind(), doc.GetName())
			continue
		}
		if !slices.Contains(supportedKinds, doc.GetKind()) {
			return fmt.Errorf(
				"%s %s: the kind %s is not expected in the bundle, check olm/values-olm.yaml",
				doc.GetKind(), doc.GetName(), doc.GetKind())
		}
		removePlaceholderAnnotation(doc)
		doc.SetLabels(updateLabels(doc.GetLabels(), version))
		if doc.GetKind() == "MutatingWebhookConfiguration" || doc.GetKind() == "ValidatingWebhookConfiguration" {
			reportIgnoredNamespaceSelectors(doc, log)
		}
		if doc.GetKind() == kindCustomResourceDefinition {
			if err = serveOnlyStorageVersion(doc, log); err != nil {
				return fmt.Errorf("the CustomResourceDefinition %s: %w", doc.GetName(), err)
			}
		}
		if doc.GetKind() == kindDeployment {
			deployments++
			if err = transformDeployment(doc, version, log); err != nil {
				return fmt.Errorf("the Deployment %s: %w", doc.GetName(), err)
			}
		}
		kept = append(kept, doc)
	}
	if deployments != 1 {
		return fmt.Errorf("expected exactly one Deployment, found %d", deployments)
	}
	return writeDocuments(out, kept)
}

func readDocuments(in io.Reader) ([]*unstructured.Unstructured, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(in))
	var docs []*unstructured.Unstructured
	for {
		raw, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read the input: %w", err)
		}
		jsonDoc, err := yaml.YAMLToJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse a document: %w", err)
		}
		if trimmed := bytes.TrimSpace(jsonDoc); len(trimmed) == 0 || string(trimmed) == "null" {
			// a template that renders nothing but the "# Source:" comment
			continue
		}
		doc := &unstructured.Unstructured{}
		if err = doc.UnmarshalJSON(jsonDoc); err != nil {
			return nil, fmt.Errorf("cannot parse a document: %w", err)
		}
		docs = append(docs, doc)
	}
}

func writeDocuments(out io.Writer, docs []*unstructured.Unstructured) error {
	for _, doc := range docs {
		data, err := yaml.Marshal(doc.Object)
		if err != nil {
			return fmt.Errorf("%s %s: cannot serialize the document: %w", doc.GetKind(), doc.GetName(), err)
		}
		if bytes.Contains(data, []byte(placeholder)) {
			return fmt.Errorf("%s %s still contains the placeholder %q", doc.GetKind(), doc.GetName(), placeholder)
		}
		if _, err = fmt.Fprintf(out, "---\n%s", data); err != nil {
			return err
		}
	}
	return nil
}

func removePlaceholderAnnotation(doc *unstructured.Unstructured) {
	annotations := doc.GetAnnotations()
	if _, ok := annotations[placeholderAnnotation]; !ok {
		return
	}
	delete(annotations, placeholderAnnotation)
	if len(annotations) == 0 {
		annotations = nil
	}
	doc.SetAnnotations(annotations)
}

// reportIgnoredNamespaceSelectors warns about webhooks with a namespaceSelector. The webhookdefinitions of a CSV have
// no namespaceSelector, OLM sets the selector of the OperatorGroup instead (none for AllNamespaces).
func reportIgnoredNamespaceSelectors(doc *unstructured.Unstructured, log io.Writer) {
	webhooks, _, _ := unstructured.NestedSlice(doc.Object, "webhooks")
	for _, item := range webhooks {
		webhook, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, hasNamespaceSelector := webhook["namespaceSelector"]; hasNamespaceSelector {
			fmt.Fprintf(log, "warning: OLM does not apply the namespaceSelector of the webhook %v in %s %s\n",
				webhook["name"], doc.GetKind(), doc.GetName())
		}
	}
}

// serveOnlyStorageVersion removes the conversion webhook and all older versions from a CRD with a conversion webhook.
// The conversion config would have to name the namespace of the webhook service, and OLM replaces the CRD on every
// upgrade before it sets its own conversion config.
func serveOnlyStorageVersion(doc *unstructured.Unstructured, log io.Writer) error {
	strategy, _, _ := unstructured.NestedString(doc.Object, "spec", "conversion", "strategy")
	if strategy != "Webhook" {
		return nil
	}
	versions, _, err := unstructured.NestedSlice(doc.Object, "spec", "versions")
	if err != nil {
		return fmt.Errorf("cannot read the versions: %w", err)
	}
	var storageVersion map[string]any
	var otherVersionNames []string
	for _, item := range versions {
		entry, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected entry in the versions: %v", item)
		}
		if entry["storage"] == true {
			storageVersion = entry
		} else {
			otherVersionNames = append(otherVersionNames, fmt.Sprint(entry["name"]))
		}
	}
	if storageVersion == nil {
		return errors.New("there is no storage version")
	}
	storageVersionName := fmt.Sprint(storageVersion["name"])
	if storageVersion["served"] != true {
		return fmt.Errorf("the storage version %s is not served", storageVersionName)
	}
	for _, name := range otherVersionNames {
		if kubeversion.CompareKubeAwareVersionStrings(name, storageVersionName) > 0 {
			return fmt.Errorf("the version %s is newer than the storage version %s and would not be served", name,
				storageVersionName)
		}
	}
	if err = unstructured.SetNestedSlice(doc.Object, []any{storageVersion}, "spec", "versions"); err != nil {
		return fmt.Errorf("cannot set the versions: %w", err)
	}
	unstructured.RemoveNestedField(doc.Object, "spec", "conversion")
	fmt.Fprintf(log, "removed the conversion webhook and all versions but the storage version %s from the "+
		"CustomResourceDefinition %s\n", storageVersionName, doc.GetName())
	return nil
}

func updateLabels(labels map[string]string, version string) map[string]string {
	for _, label := range removedLabels {
		delete(labels, label)
	}
	if _, ok := labels[versionLabel]; ok {
		labels[versionLabel] = version
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

func transformDeployment(doc *unstructured.Unstructured, version string, log io.Writer) error {
	deployment := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(doc.Object, deployment); err != nil {
		return fmt.Errorf("cannot parse the Deployment: %w", err)
	}
	deployment.Spec.Template.Labels = updateLabels(deployment.Spec.Template.Labels, version)

	podSpec := &deployment.Spec.Template.Spec
	containerIdx := slices.IndexFunc(podSpec.Containers, func(c corev1.Container) bool {
		return c.Name == managerContainerName
	})
	if containerIdx < 0 {
		return fmt.Errorf("there is no container named %s", managerContainerName)
	}
	container := &podSpec.Containers[containerIdx]

	if err := convertArgsToEnvVars(container, log); err != nil {
		return err
	}
	webhookServiceName := deployment.Name + olmWebhookServiceNameSuffix
	if err := setEnvVar(container, webhookServiceNameEnvVarName, webhookServiceName); err != nil {
		return err
	}
	fmt.Fprintf(log, "set %s to %s, the name of the webhook service OLM creates\n",
		webhookServiceNameEnvVarName, webhookServiceName)
	if err := disableAgent0Connector(container, log); err != nil {
		return err
	}
	if err := removeWebhookCertificateVolume(podSpec, container, log); err != nil {
		return err
	}
	if err := addRelatedImageEnvVars(container, log); err != nil {
		return err
	}

	transformed, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		return fmt.Errorf("cannot convert the Deployment: %w", err)
	}
	doc.Object = transformed
	return nil
}

// convertArgsToEnvVars replaces every container arg that is not in fixedArgs with the environment variable the operator
// manager reads as a fallback for the flag (see startup.EnvVarNameForFlag). Each arg is checked against the flags of
// the operator manager, since an environment variable for an unknown flag would be ignored silently at runtime. Args
// for the operator configuration resource and args with an empty value are rejected.
func convertArgsToEnvVars(container *corev1.Container, log io.Writer) error {
	flags := startup.NewOperatorManagerFlagSet()
	args := make([]string, 0, len(fixedArgs))
	for _, arg := range container.Args {
		name, value, err := parseArg(flags, arg)
		if err != nil {
			return err
		}
		if slices.Contains(fixedArgs, name) {
			args = append(args, arg)
			continue
		}
		if strings.HasPrefix(name, operatorConfigurationArgPrefix) {
			return fmt.Errorf(
				"the argument --%s is not supported in the bundle, render the chart without operator.dash0Export and "+
					"operator.exports", name)
		}
		if value == "" {
			// the operator treats an empty environment variable as unset
			return fmt.Errorf("the argument --%s has an empty value, which an environment variable cannot express", name)
		}
		envVarName := startup.EnvVarNameForFlag(name)
		if findEnvVar(container.Env, envVarName) != nil {
			return fmt.Errorf(
				"cannot convert the argument %s, the environment variable %s is already set", arg, envVarName)
		}
		container.Env = append(container.Env, corev1.EnvVar{Name: envVarName, Value: value})
		fmt.Fprintf(log, "converted the argument --%s to the environment variable %s\n", name, envVarName)
	}
	container.Args = args
	return nil
}

func parseArg(flags *flag.FlagSet, arg string) (string, string, error) {
	if !strings.HasPrefix(arg, "--") {
		return "", "", fmt.Errorf("cannot convert the argument %q, only --name and --name=value are supported", arg)
	}
	name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	f := flags.Lookup(name)
	if f == nil {
		return "", "", fmt.Errorf("the argument %s is not a command-line flag of the operator manager", arg)
	}
	if !hasValue {
		if boolFlag, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !boolFlag.IsBoolFlag() {
			return "", "", fmt.Errorf("the argument %s has no value", arg)
		}
		value = "true"
	}
	if err := flags.Set(name, value); err != nil {
		return "", "", fmt.Errorf("the argument %s has an invalid value: %w", arg, err)
	}
	return name, value, nil
}

// disableAgent0Connector turns off the agent0-connector, which is rendered with enabled=true so that the bundle carries
// its RBAC rules. It is switched on via the Subscription.
func disableAgent0Connector(container *corev1.Container, log io.Writer) error {
	if err := setEnvVar(container, agent0ConnectorEnabledEnvVarName, "false"); err != nil {
		return fmt.Errorf("%w, render the chart with operator.agent0Connector.enabled=true", err)
	}
	container.Env = slices.DeleteFunc(container.Env, func(envVar corev1.EnvVar) bool {
		return slices.Contains(agent0ConnectorSettingEnvVarNames, envVar.Name)
	})
	fmt.Fprintf(log, "set %s to false and removed the agent0-connector settings\n", agent0ConnectorEnabledEnvVarName)
	return nil
}

// removeWebhookCertificateVolume removes the volume the chart mounts at the path where OLM mounts the webhook serving
// certificate.
func removeWebhookCertificateVolume(podSpec *corev1.PodSpec, container *corev1.Container, log io.Writer) error {
	var removed []string
	container.VolumeMounts = slices.DeleteFunc(container.VolumeMounts, func(mount corev1.VolumeMount) bool {
		if mount.MountPath == webhookServingCertsPath {
			removed = append(removed, mount.Name)
			return true
		}
		return false
	})
	if len(removed) == 0 {
		return fmt.Errorf("no volume is mounted at %s", webhookServingCertsPath)
	}
	for _, c := range slices.Concat(podSpec.InitContainers, podSpec.Containers) {
		for _, mount := range c.VolumeMounts {
			if slices.Contains(removed, mount.Name) {
				return fmt.Errorf("the container %s also mounts the volume %s", c.Name, mount.Name)
			}
		}
	}
	podSpec.Volumes = slices.DeleteFunc(podSpec.Volumes, func(volume corev1.Volume) bool {
		return slices.Contains(removed, volume.Name)
	})
	fmt.Fprintf(log, "removed the volume %s, OLM mounts the webhook certificate at %s\n",
		strings.Join(removed, ", "), webhookServingCertsPath)
	return nil
}

// addRelatedImageEnvVars adds a RELATED_IMAGE_* variable for every DASH0_*_IMAGE variable. operator-sdk derives
// spec.relatedImages of the CSV from these variables and discards the relatedImages of the CSV base.
func addRelatedImageEnvVars(container *corev1.Container, log io.Writer) error {
	var relatedImages []corev1.EnvVar
	for _, envVar := range container.Env {
		match := imageEnvVarNamePattern.FindStringSubmatch(envVar.Name)
		if match == nil {
			continue
		}
		if envVar.Value == "" {
			return fmt.Errorf("the environment variable %s has no literal value, which a related image needs", envVar.Name)
		}
		name := relatedImageEnvVarPrefix + match[1]
		if findEnvVar(container.Env, name) != nil {
			return fmt.Errorf("the environment variable %s is already set", name)
		}
		relatedImages = append(relatedImages, corev1.EnvVar{Name: name, Value: envVar.Value})
		fmt.Fprintf(log, "added %s for %s\n", name, envVar.Name)
	}
	container.Env = append(container.Env, relatedImages...)
	return nil
}

func setEnvVar(container *corev1.Container, name string, value string) error {
	envVar := findEnvVar(container.Env, name)
	if envVar == nil {
		return fmt.Errorf("the environment variable %s is missing", name)
	}
	envVar.Value = value
	envVar.ValueFrom = nil
	return nil
}

func findEnvVar(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}
