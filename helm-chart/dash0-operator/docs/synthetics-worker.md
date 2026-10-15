# Dash0 Synthetics Worker (Private Locations)

The synthetics-worker runs Dash0 synthetic checks from inside your own Kubernetes cluster, so that checks can reach
internal services that Dash0's public locations cannot. It dials outbound only (no inbound ports are opened), polls
Dash0 for work, and executes synthetic checks against a Dash0-issued *private location*.

A cluster can shard checks across multiple Dash0 private locations: each private location is configured as one
*instance*, and the operator runs one Kubernetes Deployment per instance.

## Quickstart

Enabling the synthetics-worker takes two steps: turning the feature flag on in the Helm chart, and configuring at
least one instance on the `Dash0OperatorConfiguration` resource.

1. Install or upgrade the Helm chart with the feature enabled and the Dash0 synthetics backend address set:

   ```shell
   helm upgrade --install dash0-operator dash0-operator/dash0-operator \
     --namespace dash0-system \
     --set operator.syntheticsWorker.enabled=true \
     --set operator.syntheticsWorker.serverAddress=ingress.<cell>.dash0.com:443
   ```

   The exact `serverAddress` for your organization is shown in the Dash0 UI when you create a private location.

2. Configure at least one instance on your `Dash0OperatorConfiguration` resource. The private location ID and the
   authorization token are per-cluster settings that live on this resource (not on the Helm chart), so that they can
   be changed at runtime without a `helm upgrade`:

   ```yaml
   apiVersion: operator.dash0.com/v1alpha1
   kind: Dash0OperatorConfiguration
   metadata:
     name: dash0-operator-configuration-resource
   spec:
     # ... your existing exports, etc. ...
     syntheticsWorker:
       instances:
         - locationId: my-cluster-location
           authorization:
             token: auth_...
   ```

   `locationId` identifies which Dash0 private location this instance serves checks for, and is also used to derive
   the names of the Kubernetes resources the operator creates for it. It must be unique within `instances`.

Please refer to
[operator_configuration_types.go](https://github.com/dash0hq/dash0-operator/blob/main/api/operator/v1alpha1/operator_configuration_types.go)
for the full `SyntheticsWorker`/`SyntheticsWorkerInstance` schema, including per-instance `replicas`, `resources`,
`nodeAffinity` and `tolerations`.

## How it works

For each configured instance, the operator creates one Deployment and one ServiceAccount in the operator's own
namespace. The authorization token is passed to the pod as an environment variable, either from the literal
`authorization.token` value or, via `authorization.secretRef`, from a Kubernetes secret — the operator never reads
that secret itself; Kubernetes substitutes it into the pod's environment directly.

Every instance's NodeAffinity defaults to the same node selection used by the operator's other workloads (excluding
nodes labeled `dash0.com/enable=false`, and requiring `kubernetes.io/os=linux`) unless you set `nodeAffinity`
explicitly on that instance.

## Opting Out

Two independent opt-outs exist:

- **Per-cluster, at runtime:** set `spec.syntheticsWorker.enabled: false` on the `Dash0OperatorConfiguration`
  resource. The operator removes every synthetics-worker Deployment and ServiceAccount it manages. Re-enabling (or
  removing the field, since it defaults to `true`) redeploys them.
- **At the Helm level:** set `operator.syntheticsWorker.enabled=false` and run `helm upgrade`. This disables the
  feature chart-wide; note that the Helm chart does not remove already-deployed synthetics-worker resources on its
  own when you flip this flag — uninstall and reinstall the operator if you need them removed immediately.

## Status

The operator reports the aggregate and per-instance state on `Dash0OperatorConfiguration.status.syntheticsWorker`:
`deployed`/`reason`/`message` for whether every instance was successfully created or updated, and `ready`/`readyReason`
for whether every instance has all of its desired replicas ready. `status.syntheticsWorker.instances` breaks both down
per `locationId`.

## Limitations

- **Requires a manually managed operator configuration resource.** Instances have no Helm-level configuration, so
  they must be set directly on the `Dash0OperatorConfiguration` resource. This means the synthetics-worker currently
  cannot be used together with `operator.dash0Export.enabled=true`, which has the Helm chart create and manage that
  resource automatically; install with `operator.dash0Export.enabled=false` and create the resource yourself instead
  (see [Configuration](configuration.md)).
- **Not yet supported on GKE Autopilot.** GKE Autopilot requires every container image to be explicitly allowlisted;
  the synthetics-worker image has not been added to that allowlist yet.

## Related Documentation

* [Configuration](configuration.md) - Backend connections and operator configuration basics
* [Advanced Configuration](advanced-configuration.md) - Node affinity, tolerations, and resource tuning
* [Platform Specific](platform-specific.md) - GKE Autopilot and other platform notes
