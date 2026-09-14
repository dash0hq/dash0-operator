// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

// This test hands every collector configuration of the matrix in collector_configs_matrix_test.go to the collector
// binary's validate subcommand, which unmarshals the configuration component by component and rejects unknown keys.
// This makes sure that the operator's collectors start correctly with all possible rendered collector configurations.
// It is regression test for issues like https://github.com/dash0hq/dash0-operator/pull/1388.
// (The assertions of collector_config_maps_test.go only check the rendered YAML against expectations of this
// repository, they will not catch issues when the OpenTelemetry collector project removes or renames a setting.)
//
// This test is not part of the unit test suite for this package. It is skipped unless RUN_COLLECTOR_CONFIGS_VALIDATION
// is set to true; as it needs the collector images and runs docker commands.
//
// Run it via `make collector-configs-validate`. CI runs this step as part of the ci.yaml workflow, as a separate job
// ("collector_configs_validation").

const (
	runValidationEnvVarName               = "RUN_COLLECTOR_CONFIGS_VALIDATION"
	collectorImageEnvVarName              = "DASH0_COLLECTOR_IMAGE"
	signalControlCollectorImageEnvVarName = "DASH0_SIGNAL_CONTROL_COLLECTOR_IMAGE"

	// validationConcurrency is the number of collector processes that validate configurations at the same time. The
	// validate subcommand neither opens ports nor talks to an API server, so the configurations do not interfere.
	validationConcurrency = 8

	serviceAccountDirectory = "/var/run/secrets/kubernetes.io/serviceaccount"
	serviceAccountToken     = "dash0-collector-config-validation"
)

// collectorWorkloadUser is the user the collector containers run as in a cluster, see the pod security context in
// desired_state.go.
var collectorWorkloadUser = fmt.Sprintf("%d:%d", defaultUser, defaultGroup)

var (
	caCertificateOnce sync.Once
	caCertificate     string
)

// clusterEnvironment is what every collector pod has because it runs in a Kubernetes cluster, rather than something a
// configuration asks for. The resourcedetection processor needs it to build an in-cluster client configuration.
var clusterEnvironment = map[string]string{
	"KUBERNETES_SERVICE_HOST": "10.96.0.1",
	"KUBERNETES_SERVICE_PORT": "443",
}

var (
	// environmentVariableReference matches the ${env:NAME} references that the collector configuration templates
	// render.
	environmentVariableReference = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

	// environmentVariableName matches settings that name an environment variable instead of referencing its value,
	// for example the k8s_attributes processor's node_from_env_var. The collector verifies during validation that the
	// named variable is set.
	environmentVariableName = regexp.MustCompile(`(?m)^\s*\w*_from_env_var:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$`)

	// requiredDirectory matches the settings that name a directory which has to exist, that is the file_storage
	// extension's directory and the hostmetrics receiver's root_path.
	requiredDirectory = regexp.MustCompile(`(?m)^\s*(?:directory|root_path):\s*(\S+)\s*$`)
)

type validationResult struct {
	name   string
	err    error
	output string
}

func TestCollectorConfigurationsAreAcceptedByTheCollector(t *testing.T) {
	if !collectorConfigValidationIsEnabled() {
		t.Skipf(
			"skipping the validation of the collector configurations against the collector binary, set %s to true to "+
				"run it (or run `make collector-configs-validate`)",
			runValidationEnvVarName,
		)
	}
	collectorImage := os.Getenv(collectorImageEnvVarName)
	if collectorImage == "" {
		t.Fatalf("%s is set, but %s does not name a collector image to validate against",
			runValidationEnvVarName, collectorImageEnvVarName)
	}
	signalControlCollectorImage := os.Getenv(signalControlCollectorImageEnvVarName)
	if signalControlCollectorImage == "" {
		t.Fatalf("%s is set, but %s does not name a collector image to validate against",
			runValidationEnvVarName, signalControlCollectorImageEnvVarName)
	}
	RegisterTestingT(t)

	configurations := renderCollectorConfigurationMatrix(t)

	t.Logf(
		"validating %d collector configurations against %s and %s",
		len(configurations),
		collectorImage,
		signalControlCollectorImage,
	)

	results := validateCollectorConfigurations(collectorImage, signalControlCollectorImage, configurations)

	var failures []string
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v\n%s", result.name, result.err, result.output))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf(
			"%d of %d collector configurations were rejected by the collector %s:\n\n%s",
			len(failures),
			len(configurations),
			collectorImage,
			strings.Join(failures, "\n"),
		)
	}
}

// collectorConfigValidationIsEnabled reports whether RUN_COLLECTOR_CONFIGS_VALIDATION asks for the validation to run. A
// value that is not a boolean is treated like an absent one, that is the validation does not run.
func collectorConfigValidationIsEnabled() bool {
	enabled, err := strconv.ParseBool(os.Getenv(runValidationEnvVarName))
	return err == nil && enabled
}

// renderCollectorConfigurationMatrix renders all configurations of the matrix. When
// DASH0_COLLECTOR_CONFIG_OUTPUT_DIR is set, the configurations are also written there, which is meant for inspecting
// a configuration that has been rejected.
func renderCollectorConfigurationMatrix(t *testing.T) []renderedCollectorConfig {
	t.Helper()

	outputDir := os.Getenv("DASH0_COLLECTOR_CONFIG_OUTPUT_DIR")
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("cannot create the output directory %s: %v", outputDir, err)
		}
	}

	var configurations []renderedCollectorConfig
	for _, entry := range collectorConfigMatrix() {
		rendered, err := entry.render()
		if err != nil {
			t.Fatalf("cannot render the collector configurations: %v", err)
		}
		for _, configuration := range rendered {
			if configuration.content == "" {
				t.Fatalf("the rendered collector configuration %s is empty", configuration.name)
			}
			if outputDir != "" {
				path := filepath.Join(outputDir, configuration.name+".yaml")
				if err := os.WriteFile(path, []byte(configuration.content), 0644); err != nil {
					t.Fatalf("cannot write the collector configuration to %s: %v", path, err)
				}
			}
		}
		configurations = append(configurations, rendered...)
	}
	return configurations
}

func validateCollectorConfigurations(
	collectorImage string,
	signalControlCollectorImage string,
	configurations []renderedCollectorConfig,
) []validationResult {
	results := make([]validationResult, len(configurations))
	semaphore := make(chan struct{}, validationConcurrency)
	var waitGroup sync.WaitGroup

	for i, configuration := range configurations {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			image := collectorImage
			if configuration.signalControlCollector {
				image = signalControlCollectorImage
			}
			output, err := validateCollectorConfiguration(image, configuration)
			results[i] = validationResult{name: configuration.name, err: err, output: output}
		}()
	}

	waitGroup.Wait()
	return results
}

// validateCollectorConfiguration runs the collector's validate subcommand for one configuration.
func validateCollectorConfiguration(collectorImage string, configuration renderedCollectorConfig) (string, error) {
	baseArguments := []string{"run", "--rm", "--interactive", "--user", collectorWorkloadUser, "--entrypoint", "sh"}
	environment := collectorConfigurationEnvironment(configuration.content)

	// Some components require existing directories. In a cluster, these directories are volume mounts of the collector
	// pod. We use tmpfs mounts here. The container runtime creates them before the container starts, which the
	// collector's unprivileged user cannot do itself. The service account directory holds the files the script below
	// writes.
	directories := append(collectorConfigurationRequiredDirectories(configuration.content), serviceAccountDirectory)

	arguments := make([]string, 0, len(baseArguments)+2*len(environment)+2*len(directories)+3)
	arguments = append(arguments, baseArguments...)
	for _, environmentVariable := range environment {
		arguments = append(arguments, "--env", environmentVariable)
	}
	for _, directory := range directories {
		arguments = append(arguments, "--tmpfs", directory)
	}

	// Validating a configuration builds its pipelines, so the components that read the pod's service account while
	// being created need to find one: the resourcedetection processor builds an in-cluster client configuration (see
	// clusterEnvironment for the matching environment variables) and the kubeletstats receiver reads the cluster's CA
	// certificate. Nothing connects anywhere, the components only fail when these files are missing altogether.
	script := "set -e\n"
	script += fmt.Sprintf("printf '%%s' '%s' > %s/token\n", serviceAccountToken, serviceAccountDirectory)
	script += fmt.Sprintf("cat > %s/ca.crt <<'DASH0_CA_CERTIFICATE_EOF'\n%sDASH0_CA_CERTIFICATE_EOF\n",
		serviceAccountDirectory, validationCaCertificate())

	validateCommand := "exec /otelcol validate --config=/tmp/config.yaml"
	if len(configuration.featureGates) > 0 {
		validateCommand += " --feature-gates=" + strings.Join(configuration.featureGates, ",")
	}
	// The configuration is piped into the container instead of being mounted, so that the test does not depend on the
	// container runtime being allowed to bind-mount the directory the test writes to.
	script += "cat > /tmp/config.yaml\n" + validateCommand + "\n"

	arguments = append(arguments, collectorImage, "-c", script)

	command := exec.Command("docker", arguments...)
	command.Stdin = strings.NewReader(configuration.content)
	output, err := command.CombinedOutput()
	return string(output), err
}

// validationCaCertificate returns a self-signed certificate in PEM format, used as the cluster's CA certificate while
// validating configurations. It is generated once per test run and never used for an actual connection.
func validationCaCertificate() string {
	caCertificateOnce.Do(func() {
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(fmt.Sprintf("cannot generate a key for the validation CA certificate: %v", err))
		}
		certificateTemplate := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: "dash0-collector-config-validation"},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(24 * time.Hour),
			IsCA:                  true,
			KeyUsage:              x509.KeyUsageCertSign,
			BasicConstraintsValid: true,
		}
		certificate, err := x509.CreateCertificate(
			rand.Reader, certificateTemplate, certificateTemplate, &privateKey.PublicKey, privateKey)
		if err != nil {
			panic(fmt.Sprintf("cannot create the validation CA certificate: %v", err))
		}
		caCertificate = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}))
	})
	return caCertificate
}

// collectorConfigurationEnvironment provides a value for every environment variable a configuration depends on, that
// is both the ${env:...} references and the variables that settings like node_from_env_var name. The values are
// placeholders: the validate subcommand only resolves the references and unmarshals the result, it does not connect to
// anything.
func collectorConfigurationEnvironment(configuration string) []string {
	// K8S_POD_IP is rendered into listening endpoints, so it needs to be an actual address.
	values := map[string]string{
		"K8S_POD_IP": "127.0.0.1",
	}

	names := map[string]struct{}{}
	for _, expression := range []*regexp.Regexp{environmentVariableReference, environmentVariableName} {
		for _, match := range expression.FindAllStringSubmatch(configuration, -1) {
			names[match[1]] = struct{}{}
		}
	}

	environment := make([]string, 0, len(names)+len(clusterEnvironment))
	for name, value := range clusterEnvironment {
		environment = append(environment, fmt.Sprintf("%s=%s", name, value))
	}
	for name := range names {
		value, hasSpecificValue := values[name]
		if !hasSpecificValue {
			value = "dash0-collector-config-validation"
		}
		environment = append(environment, fmt.Sprintf("%s=%s", name, value))
	}
	sort.Strings(environment)
	return environment
}

// collectorConfigurationRequiredDirectories returns the directories that a configuration expects to exist. In a
// cluster these are volume mounts of the collector pod.
func collectorConfigurationRequiredDirectories(configuration string) []string {
	directories := map[string]struct{}{}
	for _, match := range requiredDirectory.FindAllStringSubmatch(configuration, -1) {
		directories[match[1]] = struct{}{}
	}

	result := make([]string, 0, len(directories))
	for directory := range directories {
		result = append(result, directory)
	}
	sort.Strings(result)
	return result
}
