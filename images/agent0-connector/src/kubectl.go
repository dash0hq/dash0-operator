// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	pb "github.com/dash0hq/dash0-operator/images/agent0-connector/proto"
)

const (
	// kubectlCommand is the only executable command requests are allowed to invoke.
	kubectlCommand = "kubectl"

	// defaultCommandTimeout is the default value for commandTimeout, bounding the execution time of a single kubectl
	// invocation.
	defaultCommandTimeout = 30 * time.Second

	// exitCodeRejected is reported for command requests that are rejected before execution (wrong command, not a
	// read-only kubectl subcommand, ...).
	exitCodeRejected int32 = 1

	// exitCodeNotExecutable is reported when kubectl could not be executed at all (e.g. the binary is missing),
	// mirroring the shell convention for "command not executable".
	exitCodeNotExecutable int32 = 127

	// exitCodeTimedOut is reported when a kubectl invocation exceeds commandTimeout. exec.CommandContext kills the
	// process with SIGKILL when the deadline is exceeded, and the OS reports a signal-killed process with exit code -1
	// (exitErr.ExitCode() returns -1 because the process did not exit normally). That is the value surfaced to the
	// backend for a timeout.
	exitCodeTimedOut int32 = -1

	// kubectlTmpEnvVarName names the environment variable that points at a writable directory for kubectl's caches.
	// kubectl stores its discovery and HTTP caches below $HOME/.kube/cache; the container's root filesystem is
	// read-only, so the operator sets this to a writable emptyDir volume (and the image defaults it to /tmp). The
	// kubectl subprocess then runs with HOME set to this directory (see kubectlEnv).
	kubectlTmpEnvVarName = "DASH0_KUBECTL_TMP"

	// maxOutputBytesPerStream caps how many bytes of stdout and stderr (each) are captured from a kubectl invocation.
	// The output is buffered in memory and sent back in a single gRPC message, so unbounded output (e.g. "kubectl get
	// -A -o yaml" on a large cluster, or a noisy "kubectl logs") could exhaust the pod's memory limit or exceed gRPC's
	// default maximum message size. Output beyond this limit is discarded and a truncation notice is appended.
	maxOutputBytesPerStream = 1024 * 1024 // 1 MiB
)

// commandTimeout bounds the execution time of a single kubectl invocation.
var commandTimeout = defaultCommandTimeout

// readOnlyKubectlSubcommands is the allowlist of kubectl subcommands the executor is allowed to run. Everything else
// is rejected. The list deliberately contains only subcommands that read cluster state and never mutate it. This is
// additional defense-in-depth on top of the strictly read-only RBAC (get & list only) granted to the agent0-connector
// service account.
var readOnlyKubectlSubcommands = map[string]struct{}{
	"api-resources": {},
	"api-versions":  {},
	"cluster-info":  {},
	"describe":      {},
	"events":        {},
	"explain":       {},
	"get":           {},
	"logs":          {},
	"top":           {},
	"version":       {},
}

// sensitiveGlobalFlags is the denylist of kubectl global flags that are rejected even for an otherwise read-only
// subcommand. These flags can redirect the request to a different cluster or identity (--server, --kubeconfig, --token,
// --context, impersonation via --as*), weaken transport security (--insecure-skip-tls-verify). The read-only RBAC
// granted to the service account remains the primary boundary; rejecting these flags is defense-in-depth so a command
// request cannot escape the intended cluster/identity.
var sensitiveGlobalFlags = map[string]struct{}{
	"--as":                       {},
	"--as-group":                 {},
	"--as-uid":                   {},
	"--certificate-authority":    {},
	"--client-certificate":       {},
	"--client-key":               {},
	"--context":                  {},
	"--insecure-skip-tls-verify": {},
	"--kubeconfig":               {},
	"--server":                   {},
	"--token":                    {},
	"-s":                         {},
}

// executeCommandRequest validates and executes a CommandRequest and returns the corresponding CommandResponse.
// Requests that do not pass validation (not kubectl, not a read-only subcommand, ...) are rejected without being
// executed and the rejection reason is reported on stderr with a non-zero exit code.
func executeCommandRequest(
	ctx context.Context,
	logger *slog.Logger,
	kubectlTmpDir string,
	req *pb.CommandRequest,
) *pb.CommandResponse {
	tc := parseTraceparent(req.GetTraceparent())
	if tc.traceID != "" {
		logger = logger.With("traceID", tc.traceID, "spanID", tc.spanID)
	}

	if err := validateCommandRequest(req); err != nil {
		logger.Warn("rejecting command request", "requestId", req.GetRequestId(), "reason", err.Error())
		return &pb.CommandResponse{
			RequestId: req.GetRequestId(),
			ExitCode:  exitCodeRejected,
			Stderr:    fmt.Sprintf("dash0 agent0-connector rejected the command: %s", err),
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	stdout := &cappedBuffer{limit: maxOutputBytesPerStream}
	stderr := &cappedBuffer{limit: maxOutputBytesPerStream}
	cmd := exec.CommandContext(execCtx, kubectlCommand, req.GetArguments()...)
	cmd.Env = kubectlEnv(kubectlTmpDir)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	resp := &pb.CommandResponse{
		RequestId: req.GetRequestId(),
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
	}
	if stdout.truncated {
		resp.Stdout = withTruncationNotice(resp.Stdout)
	}
	if stderr.truncated {
		resp.Stderr = withTruncationNotice(resp.Stderr)
	}

	// A deadline on execCtx (as opposed to the parent ctx being cancelled on shutdown) means the command exceeded
	// commandTimeout and was killed.
	timedOut := errors.Is(execCtx.Err(), context.DeadlineExceeded)

	exitCode, additionalStdErrMsg := postProcessRunResult(err, timedOut)
	resp.ExitCode = exitCode
	resp.Timeout = timedOut
	if additionalStdErrMsg != "" {
		resp.Stderr = appendLine(resp.Stderr, additionalStdErrMsg)
	}

	return resp
}

// postProcessRunResult maps the error (if any) returned by cmd.Run() to the exit code reported back to the backend.
// When the command exceeded commandTimeout (timedOut) or kubectl could not be executed at all (binary missing, ...) it
// also returns an explanatory message to append to stderr; otherwise the returned message is empty.
func postProcessRunResult(err error, timedOut bool) (exitCode int32, execFailureMessage string) {
	switch {
	case timedOut:
		return exitCodeTimedOut, fmt.Sprintf("dash0 agent0-connector aborted kubectl after the %s timeout", commandTimeout)
	case err == nil:
		return 0, ""
	case isExitError(err):
		var exitErr *exec.ExitError
		errors.As(err, &exitErr)
		return int32(exitErr.ExitCode()), ""
	default:
		return exitCodeNotExecutable, fmt.Sprintf("dash0 agent0-connector failed to execute kubectl: %s", err)
	}
}

// kubectlEnv returns the environment for the kubectl subprocess. By default, kubectl writes its discovery and HTTP
// caches to $HOME/.kube/cache, which is on the read-only root filesystem in this pod. kubectlTmpDir (resolved once at
// startup, see resolveKubectlTmpDir) points to a writable directory, so kubectl's HOME is redirected there to let
// caching work.
func kubectlEnv(kubectlTmpDir string) []string {
	return append(os.Environ(), "HOME="+kubectlTmpDir)
}

// validateCommandRequest ensures the request invokes a read-only kubectl command. It returns a non-nil error
// describing why the request is rejected, or nil if the request is allowed.
func validateCommandRequest(req *pb.CommandRequest) error {
	if req.GetCommand() != kubectlCommand {
		return fmt.Errorf("only the %q command is allowed, but got %q", kubectlCommand, req.GetCommand())
	}

	arguments := req.GetArguments()

	// Locate the subcommand, skipping any leading global flags (e.g. "get" in `kubectl -n foo get pods`), mirroring how
	// kubectl itself resolves the subcommand regardless of flag position. Invoking kubectl with no subcommand at all -
	// bare `kubectl`, or only global flags such as `kubectl --help` - is allowed: kubectl then prints its usage/help,
	// which is read-only and harmless.
	subcommand, _ := findSubcommand(arguments)
	if subcommand != "" {
		if _, allowed := readOnlyKubectlSubcommands[subcommand]; !allowed {
			return fmt.Errorf("the kubectl subcommand %q is not an allowed read-only command", subcommand)
		}
	}

	if flag, disallowed := disallowedGlobalFlag(arguments); disallowed {
		return fmt.Errorf("the kubectl flag %q is not allowed", flag)
	}

	if flag, streaming := streamingFlag(arguments); streaming {
		return fmt.Errorf("streaming/following resources (%s) is not supported", flag)
	}

	if reason, blocked := secretContentRequested(arguments); blocked {
		return errors.New(reason)
	}

	return nil
}

// findSubcommand locates the kubectl subcommand within the arguments, skipping any leading global flags (and the value
// of a value-taking flag given in the "--flag value" form, e.g. the "foo" in `kubectl -n foo get pods`). This mirrors
// how kubectl/Cobra resolves the subcommand independently of whether global flags precede it, so that "get" is found in
// both `kubectl get pods` and `kubectl -n foo get pods`. It returns the subcommand and its index in arguments, or ""
// and -1 when there is no positional argument at all (bare `kubectl`, or only flags such as `kubectl --help`).
func findSubcommand(arguments []string) (string, int) {
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		if !strings.HasPrefix(arg, "-") {
			return arg, i
		}
		// arg is a flag. Skip the following argument when this flag consumes it as its value, unless the value was
		// already supplied inline via "--flag=value".
		if !strings.Contains(arg, "=") {
			if _, takesValue := valueTakingFlags[arg]; takesValue {
				i++
			}
		}
	}
	return "", -1
}

// disallowedGlobalFlag reports whether the arguments contain a flag from the sensitiveGlobalFlags denylist (matching
// both the bare "--flag" and the "--flag=value" form), and returns the offending argument.
func disallowedGlobalFlag(arguments []string) (string, bool) {
	for _, arg := range arguments {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := arg
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if _, disallowed := sensitiveGlobalFlags[name]; disallowed {
			return arg, true
		}
	}
	return "", false
}

// streamingFlag reports whether the arguments contain a watch (-w/--watch/--watch-only) or follow (-f/--follow) flag,
// either of which would open a streaming, long-lived request that is deliberately not supported, and returns the
// offending flag.
func streamingFlag(arguments []string) (string, bool) {
	for _, arg := range arguments {
		switch {
		case arg == "-w", arg == "--watch", arg == "--watch-only", arg == "-f", arg == "--follow":
			return arg, true
		case strings.HasPrefix(arg, "--watch="), strings.HasPrefix(arg, "--watch-only="), strings.HasPrefix(arg, "--follow="):
			return arg, true
		}
	}
	return "", false
}

// allowedSecretOutputFormats are the output formats that only reveal a secret's name, type and key count, i.e. that
// allow listing secrets or checking for the presence of a particular secret without exposing the secret's data. Any
// other output format (yaml, json, jsonpath, go-template, custom-columns, ...) can serialize the secret's data and is
// therefore rejected when the request targets secrets.
var allowedSecretOutputFormats = map[string]struct{}{
	"":     {}, // the default, human-readable table output
	"name": {},
	"wide": {},
}

// valueTakingFlags lists the kubectl flags that consume the following argument as their value (when not given in
// --flag=value form). They are skipped when scanning for positional resource arguments, so that for example the
// namespace in `kubectl get pods -n secret` is not mistaken for a request targeting the secrets resource.
var valueTakingFlags = map[string]struct{}{
	"-n": {}, "--namespace": {},
	"-o": {}, "--output": {},
	"-l": {}, "--selector": {},
	"--field-selector": {},
	"--sort-by":        {},
	"--template":       {},
	"--context":        {},
	"--cluster":        {},
	"--user":           {},
	"--as":             {}, "--as-group": {}, "--as-uid": {},
	"-c": {}, "--container": {},
	"--since": {}, "--since-time": {}, "--tail": {}, "--limit-bytes": {},
	"-L": {}, "--label-columns": {},
	"--chunk-size":      {},
	"--request-timeout": {},
	"--subresource":     {},
	"--output-version":  {},
}

// secretContentRequested reports whether the kubectl arguments would read the contents of a secret, returning a
// human-readable reason when they do. Listing secrets (`kubectl get secrets`) and checking for the presence of a
// particular secret (`kubectl get secret <name>`, `kubectl describe secret <name>`) are allowed; serializing a secret's
// data via an output format such as -o yaml/json/jsonpath/go-template/custom-columns (or --template) is not. This is a
// fail-closed check: output formats that could expose the data are rejected even if a particular invocation would only
// read metadata.
func secretContentRequested(arguments []string) (string, bool) {
	if !targetsSecrets(arguments) {
		return "", false
	}
	format := outputFormat(arguments)
	if _, allowed := allowedSecretOutputFormats[format]; allowed && !hasTemplateFlag(arguments) {
		return "", false
	}
	return "reading the contents of a secret is not allowed; listing secrets or checking for the presence of a " +
		"particular secret is supported, but serializing a secret's data (e.g. via -o yaml/json/jsonpath/go-template/" +
		"custom-columns) is not", true
}

// targetsSecrets reports whether the kubectl arguments reference the secrets resource. It scans the positional resource
// arguments, skipping flags and their values. A bare resource type (e.g. "secret") is only recognized as the first
// positional argument (the resource type slot); a type/name reference (e.g. "secret/my-secret") is recognized in any
// positional slot, since `kubectl get secret/a pod/b` lists multiple type/name pairs.
func targetsSecrets(arguments []string) bool {
	_, subcommandIndex := findSubcommand(arguments)
	if subcommandIndex < 0 {
		return false
	}
	positionalIndex := 0
	// Resource references start in the argument following the subcommand (which may itself be preceded by global flags).
	for i := subcommandIndex + 1; i < len(arguments); i++ {
		arg := arguments[i]
		if strings.HasPrefix(arg, "-") {
			if _, takesValue := valueTakingFlags[arg]; takesValue {
				i++ // skip the flag's value so it is not treated as a positional argument
			}
			continue
		}
		if referencesSecretType(arg, positionalIndex == 0) {
			return true
		}
		positionalIndex++
	}
	return false
}

// referencesSecretType reports whether a positional argument references the secrets resource type. The argument may be
// a comma-separated list of resources (e.g. "secret,configmap") and each entry may be a bare type or a type/name pair
// (e.g. "secret/my-secret"). A bare type only counts as a resource type when it is the first positional argument;
// type/name pairs always denote a resource type.
func referencesSecretType(token string, firstPositional bool) bool {
	for _, part := range strings.Split(token, ",") {
		resourceType := part
		typeName := false
		if idx := strings.Index(part, "/"); idx >= 0 {
			resourceType = part[:idx]
			typeName = true
		}
		if (typeName || firstPositional) && isSecretResourceType(resourceType) {
			return true
		}
	}
	return false
}

// isSecretResourceType reports whether the given resource type refers to secrets, accepting the singular and plural
// forms as well as fully qualified forms such as "secrets.v1." (the API group/version suffix is ignored).
func isSecretResourceType(resourceType string) bool {
	resourceType = strings.ToLower(resourceType)
	if idx := strings.Index(resourceType, "."); idx >= 0 {
		resourceType = resourceType[:idx] // strip the API group/version: "secrets.v1." -> "secrets"
	}
	return resourceType == "secret" || resourceType == "secrets"
}

// outputFormat returns the normalized output format requested via -o/--output (handling the "-o yaml", "-o=yaml",
// "-oyaml" and "--output=yaml" forms), or "" if none is set. For composite formats it returns the base type, e.g.
// "jsonpath" for "jsonpath={.data}" and "custom-columns" for "custom-columns=NAME:.metadata.name".
func outputFormat(arguments []string) string {
	for i := 0; i < len(arguments); i++ {
		arg := arguments[i]
		switch {
		case arg == "-o" || arg == "--output":
			if i+1 < len(arguments) {
				return normalizeOutputFormat(arguments[i+1])
			}
		case strings.HasPrefix(arg, "--output="):
			return normalizeOutputFormat(strings.TrimPrefix(arg, "--output="))
		case strings.HasPrefix(arg, "-o="):
			return normalizeOutputFormat(strings.TrimPrefix(arg, "-o="))
		case strings.HasPrefix(arg, "-o"):
			return normalizeOutputFormat(strings.TrimPrefix(arg, "-o"))
		}
	}
	return ""
}

// normalizeOutputFormat reduces an output format value to its lower-cased base type, dropping any "=..." suffix used by
// composite formats (jsonpath, go-template, custom-columns).
func normalizeOutputFormat(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "="); idx >= 0 {
		value = value[:idx]
	}
	return strings.ToLower(value)
}

// hasTemplateFlag reports whether the arguments contain the --template flag, which selects go-template output and can
// therefore expose a secret's data.
func hasTemplateFlag(arguments []string) bool {
	for _, arg := range arguments {
		if arg == "--template" || strings.HasPrefix(arg, "--template=") {
			return true
		}
	}
	return false
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// appendLine appends line to existing, separating them with a newline if existing is non-empty.
func appendLine(existing, line string) string {
	if existing == "" {
		return line
	}
	return existing + "\n" + line
}

// withTruncationNotice appends a notice that the output was truncated at maxOutputBytesPerStream bytes.
func withTruncationNotice(output string) string {
	return appendLine(
		output,
		fmt.Sprintf("[dash0 agent0-connector truncated the output at %d bytes]", maxOutputBytesPerStream),
	)
}

// cappedBuffer is an io.Writer that captures up to limit bytes and discards the rest, recording whether any data was
// discarded. It always reports the full write length so the kubectl subprocess never sees a short write (which os/exec
// would surface as an error); the discarded bytes are simply dropped.
type cappedBuffer struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}
