// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/exporters"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type otlpExporter struct {
	Name               string
	Endpoint           string
	Authorization      *dash0ExporterAuthorization
	Headers            []dash0common.Header
	HeaderEnvVars      []headerSecretEnvVar
	Encoding           string
	Insecure           bool
	InsecureSkipVerify bool
	Keepalive          *dash0common.KeepaliveClientConfig
	BalancerName       string
}

type defaultOtlpExporters = []otlpExporter

type namespacedOtlpExporters = map[string][]otlpExporter

type otlpExporters struct {
	Default    defaultOtlpExporters
	Namespaced namespacedOtlpExporters
}

// headerSecretEnvVar links a generated collector environment variable name to the Kubernetes secret key from which its
// value is sourced. The environment variable is added to the collector pod and referenced from the rendered header
// value via ${env:EnvVarName}.
type headerSecretEnvVar struct {
	EnvVarName   string
	SecretKeyRef *dash0common.SecretKeySelector
}

func (e *otlpExporters) AllNamespaceKeys() []string {
	keys := make([]string, 0, len(e.Namespaced))
	for k := range e.Namespaced {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (e *otlpExporters) allNamespacedExporters() []otlpExporter {
	all := make([]otlpExporter, 0, len(e.Namespaced))
	for _, k := range e.AllNamespaceKeys() {
		all = append(all, e.Namespaced[k]...)
	}
	return all
}

func (e *otlpExporters) allExporters() []otlpExporter {
	nsExporters := e.allNamespacedExporters()
	all := make([]otlpExporter, 0, len(e.Default)+len(nsExporters))
	all = append(all, e.Default...)
	all = append(all, nsExporters...)
	return all
}

const (
	dash0ExporterNamePrefix = "otlp_grpc/dash0"
	grpcExporterNamePrefix  = "otlp_grpc"
	httpExporterNamePrefix  = "otlp_http"
)

func getDefaultOtlpExporters(dash0Config *dash0v1alpha1.Dash0OperatorConfiguration) ([]otlpExporter, error) {
	if !dash0Config.HasExportsConfigured() {
		return nil, nil
	}

	exporters := make([]otlpExporter, 0, dash0Config.ExportsCount())
	for i, export := range dash0Config.EffectiveExports() {
		exp, err := convertExportSettingsToExporterList(&export, i, true, nil)
		if err != nil {
			return nil, err
		} else {
			exporters = append(exporters, exp...)
		}
	}
	return exporters, nil
}

// getNamespacedOtlpExporters will log and continue on errors. If a namespace has invalid exporter settings, the
// remaining namespaces will still be handled correctly. For the invalid namespace, the default exporters will be used.
func getNamespacedOtlpExporters(
	allMonitoringResources []dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) namespacedOtlpExporters {
	nsExporters := make(map[string][]otlpExporter, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		if !monitoringResource.HasExportsConfigured() {
			continue
		}
		ns := monitoringResource.Namespace

		exporters := make([]otlpExporter, 0, monitoringResource.ExportsCount())
		for i, export := range monitoringResource.EffectiveExports() {
			exp, err := convertExportSettingsToExporterList(&export, i, false, &ns)
			if err != nil {
				logger.ErrorTelemetryCollectionIssue(err, fmt.Sprintf("Custom exporters for namespace %s could not be applied. "+
					"Default exporters will be used for this namespace.", ns))
				break
			} else {
				exporters = append(exporters, exp...)
			}
		}
		nsExporters[ns] = exporters
	}
	return nsExporters
}

func convertExportSettingsToExporterList(
	export *dash0common.Export,
	index int,
	isDefault bool,
	namespace *string,
) ([]otlpExporter, error) {
	if export == nil {
		return nil, nil
	}
	if !isDefault && namespace == nil {
		return nil, fmt.Errorf("no valid nameSuffix provided for namespaced exporter, unable to create OpenTelemetry collector")
	}

	var nameSuffix string
	if isDefault {
		nameSuffix = exporters.NameSuffixDefault(index)
	} else {
		nameSuffix = exporters.NameSuffixNs(index, *namespace)
	}

	var exporterList []otlpExporter

	if export.Dash0 == nil && export.Grpc == nil && export.Http == nil {
		return nil, fmt.Errorf("%s no exporter configuration found", commonExportErrorPrefix)
	}

	if export.Dash0 != nil {
		var auth *dash0ExporterAuthorization
		var err error
		if isDefault {
			auth, err = dash0ExporterAuthorizationForExport(*export, index, isDefault, nil)
		} else {
			auth, err = dash0ExporterAuthorizationForExport(*export, index, isDefault, namespace)
		}
		if err != nil {
			return nil, err
		}

		dash0Exporter, err := convertDash0ExporterToOtlpExporter(export.Dash0, nameSuffix, auth)
		if err != nil {
			return nil, err
		}
		exporterList = append(exporterList, *dash0Exporter)
	}

	if export.Grpc != nil {
		grpcExporter, err := convertGrpcExporterToOtlpExporter(export.Grpc, nameSuffix)
		if err != nil {
			return nil, err
		}
		exporterList = append(exporterList, *grpcExporter)
	}

	if export.Http != nil {
		httpExporter, err := convertHttpExporterToOtlpExporter(export.Http, nameSuffix)
		if err != nil {
			return nil, err
		}
		exporterList = append(exporterList, *httpExporter)
	}

	return exporterList, nil
}

func convertDash0ExporterToOtlpExporter(
	d0 *dash0common.Dash0Configuration,
	nameSuffix string,
	auth *dash0ExporterAuthorization,
) (*otlpExporter, error) {
	if d0.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
	} else if auth == nil {
		return nil, fmt.Errorf("no authorization provided for the Dash0 exporter, unable to create the OpenTelemetry collector")
	}
	headers := []dash0common.Header{
		{
			Name:  util.AuthorizationHeaderName,
			Value: authHeaderValue(auth.EnvVarName),
		},
	}
	if d0.Dataset != "" && d0.Dataset != util.DatasetDefault {
		headers = append(headers, dash0common.Header{
			Name:  util.Dash0DatasetHeaderName,
			Value: d0.Dataset,
		})
	}
	dash0Exporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s", dash0ExporterNamePrefix, nameSuffix),
		Endpoint:      d0.Endpoint,
		Headers:       headers,
		Authorization: auth,
		Keepalive:     d0.Keepalive,
		// For the dash0 exporter we always use pick_first, since client-side load balancing is
		// not needed and it avoids the issue of the exporter trying both the IPv4 and IPv6
		// addresses continuously, which leads to excessive warning logs since one will fail.
		// (Which one will fail, depends on whether the cluster has IPv4 or IPv6 networking.)
		BalancerName: string(dash0common.PickFirst),
	}
	setGrpcTlsFromPrefix(d0.Endpoint, &dash0Exporter)
	return &dash0Exporter, nil
}

func convertGrpcExporterToOtlpExporter(grpc *dash0common.GrpcConfiguration, nameSuffix string) (*otlpExporter, error) {
	if grpc.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the gRPC exporter, unable to create the OpenTelemetry collector")
	}
	headers, headerEnvVars, err := resolveExporterHeaders(grpc.Headers, "GRPC", nameSuffix)
	if err != nil {
		return nil, err
	}
	grpcExporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s", grpcExporterNamePrefix, nameSuffix),
		Endpoint:      grpc.Endpoint,
		Headers:       headers,
		HeaderEnvVars: headerEnvVars,
		Keepalive:     grpc.Keepalive,
		BalancerName:  string(grpc.BalancerName),
	}
	if grpc.Insecure != nil {
		grpcExporter.Insecure = *grpc.Insecure
	} else {
		setGrpcTlsFromPrefix(grpc.Endpoint, &grpcExporter)
	}
	setInsecureSkipVerify(grpc.Endpoint, grpc.InsecureSkipVerify, &grpcExporter)
	return &grpcExporter, nil
}

func convertHttpExporterToOtlpExporter(http *dash0common.HttpConfiguration, nameSuffix string) (*otlpExporter, error) {
	if http.Endpoint == "" {
		return nil, fmt.Errorf("no endpoint provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	if http.Encoding == "" {
		return nil, fmt.Errorf("no encoding provided for the HTTP exporter, unable to create the OpenTelemetry collector")
	}
	headers, headerEnvVars, err := resolveExporterHeaders(http.Headers, "HTTP", nameSuffix)
	if err != nil {
		return nil, err
	}
	encoding := string(http.Encoding)
	httpExporter := otlpExporter{
		Name:          fmt.Sprintf("%s/%s/%s", httpExporterNamePrefix, nameSuffix, encoding),
		Endpoint:      http.Endpoint,
		Headers:       headers,
		HeaderEnvVars: headerEnvVars,
		Encoding:      encoding,
	}
	setInsecureSkipVerify(http.Endpoint, http.InsecureSkipVerify, &httpExporter)
	return &httpExporter, nil
}

func setInsecureSkipVerify(endpoint string, insecureSkipVerify *bool, exporter *otlpExporter) {
	if !hasNonTlsPrefix(endpoint) && pointers.ReadBoolPointerWithDefault(insecureSkipVerify, false) {
		exporter.InsecureSkipVerify = true
	}
}

func authHeaderValue(envVarName string) string {
	return fmt.Sprintf("Bearer ${env:%s}", envVarName)
}

// resolveExporterHeaders translates the configured headers of a gRPC or HTTP exporter into the headers rendered into
// the collector configuration plus the list of environment variables that need to be added to the collector pod.
// Headers with a literal value are passed through unchanged. Headers whose value is sourced from a Kubernetes secret
// (valueFrom.secretKeyRef) are rendered as a ${env:...} reference, and a corresponding environment variable (backed by
// the secret) is returned so the collector can resolve the value at runtime. The protocol ("GRPC" or "HTTP") and the
// exporter's nameSuffix are used to derive a collector-pod-wide unique environment variable name per header.
func resolveExporterHeaders(
	headers []dash0common.Header,
	protocol string,
	nameSuffix string,
) ([]dash0common.Header, []headerSecretEnvVar, error) {
	if len(headers) == 0 {
		return nil, nil, nil
	}
	resolvedHeaders := make([]dash0common.Header, 0, len(headers))
	var headerEnvVars []headerSecretEnvVar
	for i, header := range headers {
		hasValue := header.Value != ""
		hasValueFrom := header.ValueFrom != nil
		if hasValue == hasValueFrom {
			return nil, nil, fmt.Errorf(
				"%s header %q must have exactly one of value or valueFrom set", commonExportErrorPrefix, header.Name)
		}
		if !hasValueFrom {
			resolvedHeaders = append(resolvedHeaders, header)
			continue
		}
		if header.ValueFrom.SecretKeyRef == nil ||
			header.ValueFrom.SecretKeyRef.Name == "" ||
			header.ValueFrom.SecretKeyRef.Key == "" {
			return nil, nil, fmt.Errorf(
				"%s header %q valueFrom must reference a secret name and key", commonExportErrorPrefix, header.Name)
		}
		envVarName := exporters.HeaderSecretEnvVarName(protocol, nameSuffix, i)
		resolvedHeaders = append(resolvedHeaders, dash0common.Header{
			Name:  header.Name,
			Value: fmt.Sprintf("${env:%s}", envVarName),
		})
		headerEnvVars = append(headerEnvVars, headerSecretEnvVar{
			EnvVarName:   envVarName,
			SecretKeyRef: header.ValueFrom.SecretKeyRef,
		})
	}
	return resolvedHeaders, headerEnvVars, nil
}

// validateExporterSecretRefs verifies that every Kubernetes secret referenced by the given exporters (the Dash0
// authorization secretRef as well as header values sourced from secrets) exists in the operator namespace and contains
// the referenced key. Without this check, a dangling secret reference would be rolled out as a required secretKeyRef
// environment variable on the collector pods, which the kubelet answers with CreateContainerConfigError, blocking
// telemetry collection for the whole cluster.
func validateExporterSecretRefs(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	exporterList []otlpExporter,
) error {
	for _, exporter := range exporterList {
		if auth := exporter.Authorization; auth != nil {
			token := auth.Authorization.Token
			secretRef := auth.Authorization.SecretRef
			// A token literal takes precedence over the secret ref (see util.CreateEnvVarForAuthorization), only refs
			// that will actually be rendered as secretKeyRef env vars need to exist.
			if (token == nil || *token == "") && secretRef != nil && secretRef.Name != "" && secretRef.Key != "" {
				if err := validateSecretKeyRef(
					ctx, k8sClient, operatorNamespace, exporter.Name, "the authorization token",
					secretRef.Name, secretRef.Key,
				); err != nil {
					return err
				}
			}
		}
		for _, headerEnvVar := range exporter.HeaderEnvVars {
			if err := validateSecretKeyRef(
				ctx, k8sClient, operatorNamespace, exporter.Name,
				fmt.Sprintf("the value of the header environment variable %s", headerEnvVar.EnvVarName),
				headerEnvVar.SecretKeyRef.Name, headerEnvVar.SecretKeyRef.Key,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateSecretKeyRef checks that the referenced Kubernetes secret exists in the operator namespace and has the
// referenced key. This mirrors exactly what the kubelet checks when starting a container with a secretKeyRef
// environment variable; in particular, an empty value for the key is accepted. Reads are served from the operator
// manager's informer cache.
func validateSecretKeyRef(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	exporterName string,
	refPurpose string,
	secretName string,
	secretKey string,
) error {
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: secretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf(
				"the exporter %s references the Kubernetes secret \"%s\" for %s, but this secret does not exist in "+
					"the namespace of the Dash0 operator (%s); note that referenced secrets need to be created in the "+
					"operator's namespace",
				exporterName, secretName, refPurpose, operatorNamespace)
		}
		return fmt.Errorf(
			"cannot read the Kubernetes secret \"%s/%s\", referenced by the exporter %s for %s: %w",
			operatorNamespace, secretName, exporterName, refPurpose, err)
	}
	if _, hasKey := secret.Data[secretKey]; !hasKey {
		return fmt.Errorf(
			"the exporter %s references the key \"%s\" of the Kubernetes secret \"%s/%s\" for %s, but the secret "+
				"does not contain this key",
			exporterName, secretKey, operatorNamespace, secretName, refPurpose)
	}
	return nil
}

// dropNamespacedExportersWithInvalidSecretRefs removes the custom exporters of every namespace that references a
// Kubernetes secret (or secret key) that does not exist in the operator namespace, so that the default exporters are
// used for that namespace instead. This prevents a dangling secret reference in one namespace's Dash0 monitoring
// resource from breaking telemetry collection for the whole cluster (see validateExporterSecretRefs).
func dropNamespacedExportersWithInvalidSecretRefs(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	nsExporters namespacedOtlpExporters,
	logger logd.Logger,
) {
	for ns, exporterList := range nsExporters {
		if err := validateExporterSecretRefs(ctx, k8sClient, operatorNamespace, exporterList); err != nil {
			logger.ErrorTelemetryCollectionIssue(err, fmt.Sprintf("Custom exporters for namespace %s could not be "+
				"applied. Default exporters will be used for this namespace. Create the missing secret (or secret "+
				"key) in the operator namespace, then re-apply the Dash0 monitoring resource in namespace %s to "+
				"trigger a new reconciliation.", ns, ns))
			delete(nsExporters, ns)
		}
	}
}
