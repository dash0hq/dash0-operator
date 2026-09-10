# Agent0 Integration

[Agent0](https://www.dash0.com/agent0) is Dash0's observability and SRE assistant. It investigates incidents, analyzes
telemetry across signals, and answers questions about your systems.

Sometimes telemetry alone does not tell the full story though.
When a pod is restarting, a workload is not receiving traffic, or a rollout is stuck, the answer is in the state of the
cluster: resource limits, readiness probes, node conditions, Kubernetes events, and so on.
Without access to that state, Agent0 can describe the symptom, but it might not be able to find the root cause.

The Dash0 operator's Agent0 integration closes that gap.
It lets Agent0 run **read-only** `kubectl` commands in your cluster, on demand, while an investigation is running —
the same commands you would run yourself, executed from inside the cluster and returned to Agent0 as if it had a
terminal attached to it.

> **Note:** The Agent0 integration is currently experimental. It is disabled by default.

## Table of Contents

- [How It Works](#how-it-works)
- [Benefits](#benefits)
- [Security](#security)
- [Enabling the Integration](#enabling-the-integration)
- [Opting Out via the Dash0OperatorConfiguration Resource](#opting-out-via-the-dash0operatorconfiguration-resource)
- [Verifying the Setup](#verifying-the-setup)
- [Disabling the Integration Again](#disabling-the-integration-again)

## How It Works

When the integration is enabled, the operator deploys one additional workload into the operator's namespace, the
**agent0-connector**.
It is a small deployment plus a dedicated service account, cluster role and cluster role binding.

The agent0-connector opens a single **outbound**, TLS-secured connection to the Dash0 backend.
It uses this connection to wait for incoming `kubectl` command requests from Agent0.
The agent0-connector does not listen on any port, it does not need an ingress, and it does not require you to open your
Kubernetes API server to the internet or hand any cluster credentials to Dash0.
Clusters behind a firewall or NAT work without extra network configuration.

When Agent0 needs to look at your cluster during an investigation, it sends a `kubectl` command over that connection.
The agent0-connector validates the command, runs it inside the cluster with its own restricted read-only service
account, and returns the output and exit code.
Every cluster with a connected agent0-connector shows up to Agent0 as a `kubectl` context it can switch to, so Agent0
can work across several clusters in one investigation.

Nothing is executed proactively or on a schedule: the connector is idle until Agent0 asks a question on your behalf.

## Benefits

* **The desired state, not just the observed state.**
  The operator's collectors already report that a pod was OOM-killed, that a rollout happened, or that a node went
  `NotReady`, as metrics and as Kubernetes events.
  What no telemetry signal carries is the manifest behind it: the memory limit that is too low, the readiness probe
  with a one-second timeout, the image tag that changed, the node selector that nothing matches.
  That is what turns a symptom into a root cause, and being able to read it is what the Agent0 integration adds.
* **Resource types that emit no telemetry at all.**
  Services and their selectors, endpoint slices, ingresses, network policies, persistent volume claims and storage
  classes, resource quotas and limit ranges, horizontal pod autoscalers, pod disruption budgets, RBAC objects,
  admission webhook configurations, node taints and allocatable capacity.
  A service whose selector matches nothing, or a network policy that drops the traffic, produces no telemetry saying
  so — it is only visible in the resources themselves.
* **No manual copy-and-paste.**
  No more pasting `kubectl describe` output into a chat window to get help interpreting it.
* **No inbound access to your cluster.**
  The connection is established from inside your cluster, outbound only.
  Your API server stays private and no kubeconfig or API server credential ever leaves the cluster.
* **Namespaces without telemetry.**
  Log and event collection are enabled per namespace, so a namespace nobody has onboarded yet is exactly the one with
  nothing to look at in Dash0.
  Agent0 can still inspect it if you ask it to, which also makes "why is this namespace not sending any data" a question
  it can answer.
* **You stay in control of the level of access Agent0 has.**
  The permissions Agent0 effectively has in your cluster are Kubernetes RBAC rules that you can inspect, and narrow (or
  widen) at any time.
* **Multi-cluster investigations.**
  Each cluster running an agent0-connector is available to Agent0 as a separate context.
* **Diagnostics for the operator itself.**
  The default permissions include the Dash0 custom resources and the third-party resources the operator reconciles, so
  Agent0 can also help troubleshoot your Dash0 operator setup.

## Security

Giving an AI assistant access to a production cluster is a decision that deserves scrutiny.
The integration is therefore built in layers:
* the RBAC rules bound what is possible at the Kubernetes level,
* the built-in command validation bounds what the agent0-connector will even attempt, and
* the built-in secret redaction bounds what can leave the cluster, preventing secret exfiltration via the Agent0
  integration.

### Read-Only by Design

The service account of the agent0-connector is granted the verbs `get` and `list` only.
There is no `create`, `update`, `patch`, `delete`, `watch`, `exec`, or `port-forward`, and no wildcard verb.
Agent0 cannot change anything in your cluster through this integration, and it cannot get a shell into any of your
containers.

This read-only restriction is enforced, not merely documented: if you configure custom RBAC rules (see below) that
contain any other verb, this is considered a validation error and the Helm installation fails.

By default, the connector has **no access to Kubernetes secrets and no access to ConfigMaps** at all.

### Customizable RBAC Rules

By default, the operator grants read-only access to a fixed set of well-known resource types: pods and pod logs, the
workload resource types, namespaces, nodes, services and other core resources, the networking and storage resource
types, RBAC objects, resource metrics for `kubectl top`, the Dash0 custom resources, and the Prometheus and Perses
custom resources the operator reconciles.
You can review the default resource types it has access to
[here](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/files/agent0-connector-default-cluster-role-rules.yaml).

You can replace that list entirely with the Helm value `operator.agent0Connector.clusterRole.rules`, either to grant
access to fewer resource types, or to add a resource type the defaults do not cover.
See [Restricting or Widening the Permissions](#restricting-or-widening-the-permissions) below.

The default rules grant no access to non-resource URLs, since `kubectl`'s API discovery is normally covered by the
`system:discovery` cluster role, which is bound to the group `system:authenticated`.
On a cluster where that binding has been removed (a common hardening step), API discovery fails and no command works at
all — with the default rules just as much as with custom ones.
The fix in that case is to provide custom rules that include a rule with `nonResourceURLs: ["*"]` and `verbs: ["get"]`.

### Command Validation

On top of RBAC, the agent0-connector validates every incoming command before executing it.
This is defense in depth: even a request that RBAC would permit is rejected when it does not fit the read-only
contract.

* **Only `kubectl`.**
  No other executable can be invoked.
  There is no shell, no pipe, no redirection.
* **Only read-only `kubectl` commands.**
  The allowlist is `api-resources`, `api-versions`, `auth`, `cluster-info`, `describe`, `events`, `explain`, `get`,
  `logs`, `top` and `version`.
  Everything else — `apply`, `delete`, `edit`, `exec`, `port-forward`, `scale`, `patch`, and any command a future
  `kubectl` release might add — is rejected.
* **Subcommands are restricted where needed.**
  `auth` is only allowed as `auth can-i`, never as `auth reconcile`, which would write roles and role bindings.
  `cluster-info` is only allowed bare, never as `cluster-info dump`.
* **Flags are allowlisted.**
  Rejected are flags that would redirect the request (`--raw`), select a different cluster or identity (`--server`,
  `--kubeconfig`, `--context`, `--as`, `--token`), weaken transport security, read local files (`-f`, `-k`), stream
  (`-w`, `--follow`), or log full HTTP request and response bodies (`-v`, which would expose resource contents at
  verbosity 8 and above; it is rejected at every level).
* **Output formats are allowlisted.**
  The file-based template formats (`go-template-file`, `jsonpath-file`, `custom-columns-file`) are rejected outright.
* **Secret contents are refused by the connector itself**, independent of your RBAC rules.
  Listing secrets and checking whether a particular one exists is possible if you grant access to secrets; serializing
  their data is not, and `kubectl describe secret` is rejected as well.
* **Commands are bounded.**
  Each invocation has a timeout, and the captured output is capped, so a single command cannot exhaust the pod's
  memory.

### Secret Redaction

For the resource types that can carry credentials, the connector parses the response and removes the credentials before
it is sent back.
Redacted are:

* the literal values of the environment variables of every container of any workload (pods, deployments, daemon sets,
  stateful sets, replica sets, jobs, cron jobs, controller revisions, …),
* the header values of HTTP probes and lifecycle hooks of any workload,
* all credential fields of the Dash0 custom resources — the Dash0 authorization token, exporter headers, the
  credentials of notification channel integrations, and the credentials a synthetic check sends with its request,
* the `kubectl.kubernetes.io/last-applied-configuration` annotation, which holds a verbatim copy of the applied
  manifest,
* the JSON and YAML content of ConfigMaps, if you granted access to ConfigMaps via custom RBAC rules (best effort, see
  the caveats below).

Redaction is fail-closed.
For those resource types, the connector only accepts output formats it can reliably redact — `-o json`, `-o yaml`, and
the formats that expose no content at all (`-o name`, `-o wide`, the default table output).
Formats that could reshape the response and hide a credential in the process (`-o go-template`, `-o jsonpath`,
`-o custom-columns`, `--template`) are rejected for them, and so is `kubectl describe`, whose text output cannot be
parsed reliably.
Likewise, a response that turns out not to be parseable — for example because it hit the output size cap and was
truncated — is withheld entirely rather than handed out unredacted.

The redaction of ConfigMap content is best-effort.
The connector inspects every `data` and `binaryData` value that parses as a JSON or YAML object or list and replaces
the values of the key names it knows (`token`, `password`, `headers`, `queryParameters`, `httpHeaders`).
A credential in a format it cannot parse — a properties file, a shell script — or under a key name it does not know
is returned unredacted.
Only grant access to ConfigMaps if they do not contain secrets, or if this redaction is sufficient for your setup.

#### What Is Not Redacted

Redaction covers the resource types listed above.
The content of every other resource type is returned as-is, and so are **pod logs**, which the default rules grant
access to.
If your applications log credentials, those log lines are visible to Agent0.
Likewise, if you grant access to additional resource types via custom RBAC rules, consider whether those resources can
hold sensitive values — anything that leaves the cluster is accessible to everyone who has access to your Dash0
organization and can use Agent0.

### Workload Hardening

The agent0-connector pod runs as a non-root user, with a read-only root filesystem, without privilege escalation, and
with all Linux capabilities dropped.
It has a memory limit and does not request or use persistent storage.

## Enabling the Integration

The integration needs to be enabled via the Helm chart.

```console
helm upgrade --install \
  --namespace dash0-system \
  --reuse-values \
  --set operator.agent0Connector.enabled=true \
  --set operator.agent0Connector.serverAddress=REPLACE THIS WITH THE OUTBOUND-CONNECTOR ENDPOINT, SEE BELOW \
  --set operator.agent0Connector.token=REPLACE THIS WITH YOUR DASH0 AUTH TOKEN \
  dash0-operator \
  dash0-operator/dash0-operator
```

Both `serverAddress` and a token are mandatory when `operator.agent0Connector.enabled` is `true`.
The `serverAddress` can be copied from https://app.dash0.com/settings/endpoints → Outbound Connector.
The authorization token is the same kind of token used for the configuration and telemetry export.
Auth tokens can be managed and copied from https://app.dash0.com/settings/auth-tokens.

### Helm Values

All values live under `operator.agent0Connector` and are ignored when `operator.agent0Connector.enabled` is `false`.

| Value                         | Default                      | Description                                          |
|-------------------------------|------------------------------|------------------------------------------------------|
| `enabled`                     | `false`                      | Deploys the agent0-connector, enables the feature    |
| `serverAddress`               | `""`                         | The Dash0 Outbound Connector endpoint, see above     |
| `token`                       |                              | Dash0 auth token; alternatively use `secretRef`      |
| `secretRef.name`              |                              | Name of the secret holding the auth token            |
| `secretRef.key`               |                              | Key within that secret, ignored when `token` is set  |
| `maxConcurrentCommands`       | `2`                          | How many commands run at the same time               |
| `containerResources`          | 256Mi limit / 64Mi request   | Resource settings for the agent0-connector container |
| `clusterRole.rules`           | `[]` (the built-in defaults) | Custom read-only RBAC rules, replacing the defaults  |
| `labels`, `annotations`       | `{}`                         | Additional labels and annotations for the deployment |
| `podLabels`, `podAnnotations` | `{}`                         | Additional labels and annotations for the pod        |
| `tolerations`                 | `[]`                         | Tolerations for the agent0-connector pod             |
| `nodeAffinity`                | Linux nodes not opted out    | Node affinity for the agent0-connector pod           |

### Providing the Authorization Token via a Secret

Instead of passing the token as a Helm value, you can reference a Kubernetes secret.
Create the secret in the operator's namespace:

```console
kubectl create secret generic \
  dash0-agent0-connector-authorization-secret \
  --namespace dash0-system \
  --from-literal=token=auth_...your-token-here...
```

Then reference it:

```yaml
operator:
  agent0Connector:
    enabled: true
    serverAddress: <the address provided by Dash0>
    secretRef:
      name: dash0-agent0-connector-authorization-secret
      key: token
```

If both `token` and `secretRef` are provided, `token` has priority and `secretRef` is ignored.

### Restricting or Widening the Permissions

The Helm value `operator.agent0Connector.clusterRole.rules` **replaces** the
[default rules](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/files/agent0-connector-default-cluster-role-rules.yaml), it is not merged with the default rules.
To start from the current defaults, read them from the cluster and edit the copy:

```console
kubectl get clusterrole dash0-operator-manager-agent0-connector-ro -o yaml
```

A minimal, deliberately narrow example that only allows Agent0 to look at pods and their logs, the workload resources,
and a few core resource types (namespaces, nodes, services, events):

```yaml
operator:
  agent0Connector:
    clusterRole:
      rules:
        - apiGroups: [""]
          resources: ["pods", "pods/log", "namespaces", "nodes", "services", "events"]
          verbs: ["get", "list"]
        - apiGroups: ["apps"]
          resources: ["daemonsets", "deployments", "replicasets", "statefulsets"]
          verbs: ["get", "list"]
        - apiGroups: ["authorization.k8s.io"]
          resources: ["selfsubjectaccessreviews", "selfsubjectrulesreviews"]
          verbs: ["create"]
```

A few things to keep in mind:

* Only read-only rules are accepted: `get` and `list` for any resource (exception: `create` for
  `selfsubjectaccessreviews`/`selfsubjectrulesreviews`).
  Any other verb, including the wildcard `*`, makes the Helm installation fail.
* Prefer naming resource types explicitly over granting a wildcard.
* Widening access can have security implications.
  Only grant access to a resource type when you are confident that it cannot be used to read credentials out of the
  cluster; see [Secret Redaction](#secret-redaction) for what the connector does and does not redact.
* On a cluster where the group `system:authenticated` has been removed from the `system:discovery` cluster role binding,
  add a rule with `nonResourceURLs: ["*"]` and `verbs: ["get"]`
  See [Customizable RBAC Rules](#customizable-rbac-rules) above.

### Concurrency and Sizing

`operator.agent0Connector.maxConcurrentCommands` (default: `2`) controls how many commands run at the same time.
The limit is memory-bound, not CPU-bound: a single command can peak at roughly 90 MiB, which is what the default
memory limit of 256Mi accommodates for two concurrent commands.

Raising `maxConcurrentCommands` therefore requires raising `containerResources.limits.memory` and
`containerResources.gomemlimit` accordingly, otherwise the pod can be OOM-killed when several large responses coincide.
Consider raising it when several Agent0 sessions routinely work with the same cluster at the same time.
Lowering it to `1` is not recommended: one slow command then blocks every request behind it until it times out.

### Scheduling the Pod

By default, the agent0-connector pod is scheduled on any Linux node that is not labelled `dash0.com/enable=false`.
Use `operator.agent0Connector.tolerations` and `operator.agent0Connector.nodeAffinity` to change that, for example to
keep it off tainted nodes or to pin it to a specific node pool.

## Opting Out via the Dash0OperatorConfiguration Resource

The Helm value is the master switch.
In addition, the `Dash0OperatorConfiguration` resource has an **opt-out** switch, which is useful to disable the
integration temporarily without running `helm`.

```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0OperatorConfiguration
metadata:
  name: dash0-operator-configuration-resource
spec:
  agent0Connector:
    enabled: false
```

The semantics are:

* The setting is optional.
  When it is absent, the Helm value decides.
* Setting it to `false` prevents the operator from deploying the agent0-connector, even when the integration is enabled
  via Helm.
* Setting it to `true` while the integration is disabled via Helm is a validation error.
  This resource can only opt out of the Helm value, it cannot enable the integration on its own — the connector needs
  configuration (the server address, the token, the RBAC permissions) that only the Helm chart provides, plus
  RBAC permissions that are only granted when the Helm flag is enabled.
* Using this setting is not supported when the Helm chart manages the `Dash0OperatorConfiguration` resource, that is,
  when `operator.dash0Export.enabled=true`.
  In that case, use the Helm value instead.

## Verifying the Setup

The operator records the outcome of its last attempt to deploy the agent0-connector in the status of the
`Dash0OperatorConfiguration` resource:

```console
kubectl get dash0operatorconfiguration \
  dash0-operator-configuration-resource \
  -o jsonpath='{.status.agent0Connector}'
```

`deployed: true` means the operator successfully created the agent0-connector's service account, cluster role, cluster
role binding and deployment.
If the integration is disabled via the `Dash0OperatorConfiguration` resource, it is reported as `deployed: false` with
`reason: Disabled`.
If the integration is enabled but deploying it failed, this is reported with a corresponding reason and a human-readable
message.
If the integration is disabled via Helm, the operator will not attempt to install it and will not add anything about it
to the `Dash0OperatorConfiguration` resource's status.

Note that the status reports the deployment, not the pod: a successfully created deployment can still fail to start.
Check the workload itself and its logs for that:

```console
kubectl get deployment --namespace dash0-system dash0-operator-agent0-connector
kubectl logs --namespace dash0-system deployment/dash0-operator-agent0-connector
```

(Substitute the Helm release name for the `dash0-operator` prefix if you installed the chart under a different name.)

## Disabling the Integration Again

Setting `spec.agent0Connector.enabled: false` in the `Dash0OperatorConfiguration` resource makes the operator remove
the agent0-connector and its resources again.
(This is only available in clusters where the Dash0OperatorConfiguration resource is not automatically managed by the
operator via the Helm chart.)

Flipping the Helm value `operator.agent0Connector.enabled` from `true` to `false` via `helm upgrade` behaves
differently: the operator will **not** remove the agent0-connector deployment nor its service account, cluster roles
and cluster role bindings.
It cannot, because `operator.agent0Connector.enabled=false` also removes the RBAC permissions the operator would need
to delete those resources.
The recommended path is to uninstall the operator entirely and re-install it with
`operator.agent0Connector.enabled=false`.
