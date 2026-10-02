---
name: kubectl-parsing-drift-check
description: Check whether a kubectl update makes the agent0-connector interpret kubectl command lines differently than the real kubectl binary does, and whether that opens a way to bypass the connector's validation or redaction. Use when the kubectl version in images/agent0-connector (Dockerfile and src/go.mod) changes, e.g. for a PR created by .github/workflows/update-agent0-connector-kubectl.yaml, or when images/agent0-connector/src/kubectl/parser.go changes.
---

Invoke with:

```
/kubectl-parsing-drift-check                    (compare the kubectl version on main with the one on the current branch)
/kubectl-parsing-drift-check v1.37.1 v1.38.0    (compare two explicit kubectl versions)
/kubectl-parsing-drift-check v1.38.0            (audit a single kubectl version, without a baseline)
```

# Goal

The agent0-connector receives kubectl argument lists from an untrusted source (an AI agent), validates them and then
executes them with the kubectl binary from the `registry.k8s.io/kubectl` image. The validation (`validation.go`) and the
redaction (`redaction.go`) work on the result of `parseKubectlArguments` in
`images/agent0-connector/src/kubectl/parser.go`. That function imitates how the kubectl binary resolves an argument
list, using kubectl's own command tree from the `k8s.io/kubectl` Go module.

The security property to protect is:

> For every argument list that the connector accepts, the kubectl binary executes the same command, with the same flags
> and flag values, the same positional arguments, the same resource types and the same output format that
> `parseKubectlArguments` reported.

Every difference where the connector accepts an argument list and the binary does something else (for example a
different command, a flag the connector did not see, a different flag value, a different resource type, an output
format that the redaction cannot parse, reading a local file or sending requests to a different API server) is a
**fail-open** drift and a potential security issue. A difference where the connector rejects an argument list that the
binary would accept is **fail-closed**. It is a functional limitation at most, not a security issue. Focus the analysis
on fail-open drift and report fail-closed drift only briefly.

Do not change any code unless the user asks you to. The output of this skill is a report.

# Step 1: Determine the versions

* If two versions were given in $ARGUMENTS, use them as the old and the new kubectl version.
* If one version was given, audit only that version (skip the diffing in step 3, but do all other steps).
* If no arguments were given, read the old version from `images/agent0-connector/Dockerfile` on `main`
  (`git show main:images/agent0-connector/Dockerfile`) and the new version from the working tree. If they are equal,
  audit the current version without a baseline.

kubectl `v1.X.Y` belongs to the Go modules `k8s.io/kubectl`, `k8s.io/cli-runtime` and `k8s.io/component-base` in
version `v0.X.Y`. Check that the Dockerfile and `images/agent0-connector/src/go.mod` agree on the new version (this is
what `TestKubectlBinaryAndGoModuleVersionsMatch` checks). If they do not agree, report this as a finding first,
because every other check assumes that they agree.

# Step 2: Get the sources

Work in the session's scratchpad directory, not in the repository.

* Download the Go modules for both versions with `go mod download -json k8s.io/kubectl@v0.X.Y` (likewise for
  `k8s.io/cli-runtime`, `k8s.io/component-base`, `k8s.io/client-go` and `k8s.io/apimachinery`). The `Dir` field of the
  output contains the module sources.
* Get the entry point of the kubectl binary and the `go.mod` of the Kubernetes release it was built from:
  `cmd/kubectl/kubectl.go` and `go.mod` of `kubernetes/kubernetes` at tag `v1.X.Y` (for example with
  `gh api 'repos/kubernetes/kubernetes/contents/cmd/kubectl/kubectl.go?ref=v1.X.Y' --jq .content | base64 -d`).
* Note the versions of `github.com/spf13/cobra` and `github.com/spf13/pflag` in the `go.mod` of `kubernetes/kubernetes`
  at that tag. Download these versions as well, and also the versions that the connector actually builds with
  (`go list -m github.com/spf13/cobra github.com/spf13/pflag` in `images/agent0-connector/src`).

# Step 3: Read the connector side

Read these files completely before analyzing kubectl changes:

* `images/agent0-connector/src/kubectl/parser.go`: how the argument list is resolved. Pay attention to
  `newKubectlCommandTree` (`NewKubectlCommand` instead of `NewDefaultKubectlCommandWithArgs`, built once per process),
  `findCommand`, `newRequestFlagSet` (the per-request flag set that replaces cobra's `ParseFlags`), `newStandInFlag`,
  `commandPathBelowRoot`, `extractNormalizedResourceTypes`, `normalizeResourceType` and `addKlogFlagStandIns`.
* `images/agent0-connector/src/kubectl/parsed_arguments.go`: how the parsed result is interpreted (output formats,
  `--template`).
* `images/agent0-connector/src/kubectl/validation.go`: the allowed kubectl commands (`supportedKubectlCommands`,
  `unconditionallyRejectedKubectlCommands`, `allowedSubcommandsPerKubectlCommand`), the flag allowlist
  (`allowedFlags`), the output formats (`knownOutputFormats`) and the blocked resource types (`sensitiveResourceTypes`).
* `images/agent0-connector/src/kubectl/kubectl.go`: how the binary is invoked, in particular `kubectlEnv`,
  `kubectlEnvPassThrough` and the kuberc settings.
* `images/agent0-connector/src/kubectl/allowed_commands.go`: the configurable command allowlist.
* `images/agent0-connector/Dockerfile`: what else is in the image and on `PATH`.
* `images/agent0-connector/src/kubectl/parser_test.go` and `validation_test.go`: the cases that are already covered.

Build the list of **reachable commands**: the kubectl commands and subcommands that validation can accept with some
configuration. Only these commands matter for fail-open drift. Use this list to scope all the following checks.

# Step 4: Check the drift surfaces

Check each of the following. If you have both an old and a new version, diff the relevant sources between the versions
(`diff -ru <old dir> <new dir> -- <path>`) and analyze every change. If you audit a single version, check the current
state.

## 4.1 Entry point: in-process parsing vs. the kubectl binary's `main`

The binary runs `cmd/kubectl/kubectl.go` → `cmd.NewDefaultKubectlCommand()` → `cli.RunNoErrOutput()` (from
`k8s.io/component-base/cli`) → cobra's `Command.Execute()`/`ExecuteC()`. The connector runs `NewKubectlCommand`, then
mirrors parts of `ExecuteC` itself. Compare these code paths and identify everything the binary does before or during
command resolution and flag parsing that the connector does not do, or does differently:

* Changes in `cmd/kubectl/kubectl.go` and in `component-base/cli/run.go` (flag normalization functions such as
  `WordSepNormalizeFunc`, `logs.AddFlags`, pre-run hooks).
* Changes in `NewDefaultKubectlCommandWithArgs` and `NewKubectlCommand` in `k8s.io/kubectl/pkg/cmd/cmd.go`: new root
  flags, new commands, changes to the global normalization function, new argument rewriting.
* **Plugins**: `NewDefaultKubectlCommandWithArgs` runs a `kubectl-<name>` executable from `PATH` for unknown commands
  and for unknown subcommands of the commands in `IsSubcommandPluginAllowed`. The connector does not register a plugin
  handler. Check whether `IsSubcommandPluginAllowed` or the plugin lookup changed, whether any reachable command is
  affected, and whether the image (scratch, only `/usr/local/bin/kubectl`) still contains no other executables.
* **kuberc**: aliases and default flag values from a kuberc file make the binary resolve argument lists differently. The
  connector sets `KUBERC=off` for the subprocess and does not pass the argument list to `NewKubectlCommand`. Check
  whether kuberc is still disabled by `KUBERC=off`, whether there are new sources of user preferences (other env vars,
  default file locations under `HOME`, new feature gates such as `KUBECTL_KUBERC` in
  `k8s.io/kubectl/pkg/cmd/util/helpers.go`), and whether the command tree that the connector builds (in the connector
  process, with the connector's environment) still matches the one the binary builds (in the subprocess, with the
  environment from `kubectlEnv`). Feature gates that are read from the environment are evaluated in both processes,
  and both environments differ.
* Commands that cobra adds only in `ExecuteC` (the `help` command, the `completion` command, `__complete` and
  `__completeNoDesc`): the connector does not add them, so they do not resolve. Check that this is still fail-closed.
* **Logging flags**: `addKlogFlagStandIns` copies the flags from `logs.AddFlags`. Check that the copies still have the
  same names, shorthands and `NoOptDefVal` as the flags the binary registers, and that `logs.AddFlags` did not start
  registering flags in a different way.

## 4.2 cobra and pflag

* Compare the cobra/pflag versions the kubectl binary was built with (from the `go.mod` of `kubernetes/kubernetes`) with
  the versions the connector builds with. Minimal version selection can give the connector newer versions than the
  binary. If they differ, diff cobra (`command.go`: `Find`, `findNext`, `stripFlags`, `ExecuteC`, `ParseFlags`,
  `legacyArgs`, `TraverseChildren`, command aliases, prefix matching via `EnablePrefixMatching`) and pflag (`flag.go`:
  `parseArgs`, `parseLongArg`, `parseShortArg`, `NoOptDefVal` handling, `--` handling, normalization,
  `ParseErrorsAllowlist`) between the two versions and analyze every change for differences in how an argument list is
  split into command path, flags and positional arguments.
* `newRequestFlagSet` parses each command request into a flag set of its own instead of calling cobra's `ParseFlags`,
  with copies of the resolved command's flags that carry a stand-in value. Check that it still mirrors what
  `ParseFlags` (and `mergePersistentFlags`) set up before parsing: the merged persistent flags of the parents, the help
  flag, the normalization function and `ParseErrorsAllowlist`. pflag offers no getter for `interspersed`, so
  `newRequestFlagSet` assumes the default; `TestCommandTreeOnlyUsesFlagParsingFeaturesThatNewRequestFlagSetMirrors`
  checks the command tree for that. Also check that pflag still decides how to split the argument list only from the
  fields of `pflag.Flag` (`Name`, `Shorthand`, `NoOptDefVal`, ...), which the copies keep, and not from the type or the
  behavior of the flag's `Value` (for example a type switch on `boolFlag`), which the stand-in does not reproduce.
* Check that building a kubectl command tree still has global side effects that rule out building it per request (such
  as cobra's package-level `flagCompletionFunctions` map, which is never pruned); `TestParseArgumentsDoesNotRetainMemory`
  covers the memory side of this.
* Even if the versions match, check whether kubectl started using cobra features that `findCommand` explicitly does not
  support or does not mirror (`TraverseChildren`, `EnablePrefixMatching`, `FParseErrWhitelist`,
  `DisableFlagParsing`, `Args` validators that change which arguments are positional, `PersistentPreRun` hooks that
  rewrite `os.Args` or flag values).

## 4.3 Reachable commands

For every reachable command (and its subcommands), diff its implementation in `k8s.io/kubectl/pkg/cmd/<command>` and the
shared option types it uses (`k8s.io/cli-runtime/pkg/genericclioptions`, `k8s.io/kubectl/pkg/cmd/get` for printers,
`k8s.io/cli-runtime/pkg/printers`). Look for:

* New flags, new shorthands, renamed flags or new aliases of flags. The flag allowlist matches on the long name, so a
  new shorthand for an allowed flag is fine, but an allowed long name that changes its meaning is not.
* Changes in the meaning or default value of an allowed flag, in particular flags that select the output format, read
  local files (`--filename`, `--kustomize`, `--kubeconfig`), select the API server or credentials (`--server`,
  `--token`, `--as`, `--context`, `--cluster`), select raw API access (`--raw`), or switch between server-side and
  client-side behavior.
* New output formats or changes to how `-o` values are parsed (`jsonpath=...`, `custom-columns=...`, `go-template-file`,
  `kyaml`, ...), compared with `normalizeOutputFormat` and `knownOutputFormats`. A format that renders resource content
  in a shape the redaction does not parse is fail-open if the connector accepts it.
* New subcommands or aliases of subcommands, compared with `allowedSubcommandsPerKubectlCommand`.
* Changes to which positional arguments a command accepts and how it interprets them.
* Changes to `DisableFlagParsing` on any command.

## 4.4 Resource type interpretation

The connector derives resource types from positional arguments itself (`extractNormalizedResourceTypes`,
`normalizeResourceType`), and validation blocks content requests for `sensitiveResourceTypes` based on that. The binary
resolves resource arguments with the resource builder from `k8s.io/cli-runtime/pkg/resource` (`builder.go`:
`ResourceTypeOrNameArgs`, `splitResourceTypeName`, `normalizeMultipleResourcesArgs`, `hasCombinedTypeArgs`, ...) and
the RESTMapper. Diff and check for:

* New argument forms that select a resource type (separators other than `,` and `/`, new `TYPE.VERSION.GROUP` forms,
  whitespace handling, case handling, short names, singular/plural handling).
* Forms that the binary maps to a sensitive resource type (for example `secrets`) and that `normalizeResourceType` does
  not map to the corresponding entry in `sensitiveResourceTypes`.
* New ways to select resources without positional arguments (flags such as `--filename`, `--selector`,
  `--field-selector`, `--all`, `--raw`) that bypass the resource type checks.

## 4.5 Environment and process

* New environment variables that kubectl reads (`grep -rn 'os.Getenv\|os.LookupEnv' ` in the downloaded modules) and
  whether any of them is in `kubectlEnvPassThrough` or is set in the connector process, where the in-process command
  tree is built.
* New files that kubectl reads from `HOME` (the subprocess runs with `HOME` set to the `DASH0_KUBECTL_TMP` directory)
  or from the working directory.

# Step 5: Verify each hypothesis

A source diff is only a hypothesis. For every potential fail-open drift, try to confirm or refute it with a concrete
argument list:

* Write a temporary Go test in the scratchpad or a temporary `_test.go` file in
  `images/agent0-connector/src/kubectl` that calls `parseKubectlArguments` and `validateCommandAndParseArguments` with
  the argument list, and prints what the connector resolved. Delete temporary test files afterwards.
* Run the real kubectl binary of the new version with the same argument list and observe what it does. Download the
  binary from `https://dl.k8s.io/release/v1.X.Y/bin/<os>/<arch>/kubectl` into the scratchpad. Run it with `KUBERC=off`,
  `HOME` set to an empty scratchpad directory, and a kubeconfig that points to a server that is not reachable (for
  example `https://127.0.0.1:1`), and add `-v=8` to see which request kubectl would send. Do not run it against a real
  cluster.
* A drift is confirmed if the connector accepts the argument list and the binary resolves a different command, flag,
  flag value, resource type or output format, reads a local file, or targets a different server.

# Step 6: Report

Report the result to the user:

* The compared versions (kubectl old/new, cobra and pflag for the binary and for the connector).
* **Confirmed fail-open drifts**, most severe first. For each one: the argument list, what the connector resolves, what
  the binary does, the security impact (for example credential disclosure, execution of a mutating command, access to
  local files), and a proposed fix in the connector (for example a new rejection in `validation.go` or an adjustment in
  `parser.go`) together with a regression test case for `parser_test.go` or `validation_test.go`.
* **Unconfirmed hypotheses** that you could neither confirm nor refute, with the reason.
* **Fail-closed drifts** in one short list.
* The drift surfaces from step 4 that you checked and found unchanged, in one short list, so that the user can see what
  the result covers.

If there are no confirmed fail-open drifts, say so explicitly, but still list the unconfirmed hypotheses.
