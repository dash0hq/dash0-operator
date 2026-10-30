// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const proxyPort int32 = 7070

type proxyConfiguration struct {
	yaml string
	env  []corev1.EnvVar
}

// buildProxyConfiguration derives destinations exclusively from cluster-wide Dash0 exports.
func buildProxyConfiguration(configuration *dash0v1alpha1.Dash0OperatorConfiguration) (proxyConfiguration, error) {
	exporters := map[string]any{}
	var env []corev1.EnvVar
	for _, export := range configuration.EffectiveExports() {
		if export.Dash0 == nil {
			continue
		}
		destination := export.Dash0
		endpoint := destination.Endpoint
		insecure := strings.HasPrefix(endpoint, "http://")
		if strings.Contains(endpoint, "://") {
			parsed, err := url.Parse(endpoint)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
				parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
				return proxyConfiguration{}, fmt.Errorf("invalid Dash0 debuginfo endpoint")
			}
			endpoint = parsed.Host
		}
		if host, port, err := net.SplitHostPort(endpoint); err != nil || host == "" || port == "" {
			return proxyConfiguration{}, fmt.Errorf("Dash0 debuginfo endpoint must contain a host and port")
		}
		uploadDestination := *destination
		uploadDestination.ApiEndpoint, uploadDestination.Keepalive = "", nil
		identity, err := json.Marshal(uploadDestination)
		if err != nil {
			return proxyConfiguration{}, err
		}
		id := fmt.Sprintf("%x", sha256.Sum256(identity))
		exporterName := "debuginfo/" + id
		if _, exists := exporters[exporterName]; exists {
			continue
		}
		envName := "DASH0_DEBUGINFO_TOKEN_" + strings.ToUpper(id)
		authorization, err := util.CreateEnvVarForAuthorization(destination.Authorization, envName)
		if err != nil {
			return proxyConfiguration{}, fmt.Errorf("Dash0 debuginfo destination requires authorization: %w", err)
		}
		env = append(env, authorization)
		headers := map[string]string{"authorization": "Bearer ${env:" + envName + "}"}
		if destination.Dataset != "" {
			headers["dash0-dataset"] = strings.ReplaceAll(destination.Dataset, "$", "$$")
		}
		exporters[exporterName] = map[string]any{
			"endpoint": endpoint,
			"tls":      map[string]any{"insecure": insecure},
			"headers":  headers,
		}
	}
	if len(exporters) == 0 {
		return proxyConfiguration{}, nil
	}
	if len(exporters) > 64 {
		return proxyConfiguration{}, fmt.Errorf("debuginfo uploads support at most 64 Dash0 destinations")
	}
	ids := make([]string, 0, len(exporters))
	for id := range exporters {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	slices.SortFunc(env, func(a, b corev1.EnvVar) int { return strings.Compare(a.Name, b.Name) })
	data, err := yaml.Marshal(map[string]any{
		"receivers": map[string]any{"debuginfo": map[string]any{
			"grpc": map[string]any{"endpoint": fmt.Sprintf("0.0.0.0:%d", proxyPort)},
		}},
		"exporters": exporters,
		"service": map[string]any{"pipelines": map[string]any{"debuginfo": map[string]any{
			"receivers": []string{"debuginfo"}, "exporters": ids,
		}}},
	})
	return proxyConfiguration{yaml: string(data), env: env}, err
}
