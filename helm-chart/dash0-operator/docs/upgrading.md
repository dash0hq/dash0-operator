# Upgrading and Uninstallation

This guide covers upgrading the Dash0 operator, CRD version migrations, disabling monitoring for namespaces, and uninstallation procedures.

## Table of Contents

- [Upgrading the Operator](#upgrading-the-operator)
- [CRD Version Upgrades](#crd-version-upgrades)
  - [Operator Version 0.71.0: v1alpha1 to v1beta1 Migration](#operator-version-0710-v1alpha1-to-v1beta1-migration)
- [Uninstallation](#uninstallation)
  - [Unsupported Uninstallation Procedures](#unsupported-uninstallation-procedures)

## Upgrading the Operator

To upgrade the Dash0 operator to a newer version, run the following commands:

```console
helm repo update dash0-operator
helm upgrade --wait --namespace dash0-system dash0-operator dash0-operator/dash0-operator
```

## CRD Version Upgrades

Occasionally, the custom resource definitions (CRDs) used by the Dash0 operator (Dash0OperatorConfiguration,
Dash0Monitoring) will be updated to new versions.
Whenever possible, this will happen in a way that requires no manual intervention by users.
This section contains details about CRD version updates and version migrations.

### Operator Version 0.71.0: v1alpha1 to v1beta1 Migration

With operator version 0.71.0, the Dash0 operator's `Dash0Monitoring` custom resource definition (CRD) is upgraded from
version `v1alpha1` to `v1beta1`.
The operator handles both versions correctly, that is version `v1alpha1` to `v1beta1` are both fully supported.
Here is what you need to know about this version update for `Dash0Monitoring`:

* If you have existing `Dash0Monitoring` resources in version `v1alpha1` in your cluster, they will be automatically
  converted on the fly, for example when the Dash0 operator reads the `v1alpha1` resource version.
  At some point Kubernetes might also convert the resource permanently and store it in version `v1beta1`.
* After the upgrade to version 0.71.0, you can still deploy new `Dash0Monitoring` resource in version `v1alpha1` (for
  example via `kubectl apply`).
  They will be automatically converted and stored as `v1beta1` resources by Kubernetes.
* If you want to migrate a `Dash0Monitoring` _template_ (e.g. a yaml file) from version `v1alpha1` to `v1beta1`, follow
  these steps:
    * If the template specifies the workload instrumentation mode via `spec.instrumentWorkloads`, replace that with
      `spec.instrumentWorkloads.mode`.
      That is:
      ```yaml
        spec:
          instrumentWorkloads: created-and-updated
      ```
      becomes
      ```yaml
      spec:
        instrumentWorkloads:
          mode: created-and-updated
      ```
      If the template does not specify the workload instrumentation mode explicitly (that is, it relies on using the
      default instrumentation mode), no change is necessary here.
    * If the template contains the attribute `spec.prometheusScrapingEnabled`, replace that with
      `spec.prometheusScraping.enabled`.
      That is:
      ```yaml
        spec:
          prometheusScrapingEnabled: true
      ```
      becomes
      ```yaml
      spec:
        prometheusScraping:
          enabled: true
      ```
      The attribute `spec.prometheusScraping.enabled` is also already valid for `v1alpha1`, so this particular change
      can be applied independently of the CRD version change.
      If the template does not specify whether prometheusScraping is enabled or not (that is, it relies on using the
      default value), no change is necessary here.
* We recommend to update your templates from `v1alpha1` to `v1beta1` at some point.
  However, there are currently no plans to remove support for version `v1alpha1`.
* If you want to use the new trace context propagators option that has been added in version 0.71.0 (see
  [Auto-Instrumentation](auto-instrumentation.md)), you need to use version `v1beta1` of the `Dash0Monitoring`
  resource.
  This includes updating your Yaml templates to that version, as described above.
* After upgrading to operator version 0.71.0 or later, you can no longer easily downgrade to a version before 0.71.0.
  In particular, this downgrade would require to manually delete all `Dash0Monitoring` resources in the cluster.
  The reason is that the Dash0Monitoring resources are now stored as version `v1beta1` by Kubernetes and there is no
  automatic downward conversion from `v1beta1` back to `v1alpha1`.
* The only supported version for `Dash0OperatorConfiguration` is still `v1alpha1`, that is, trying to use
  `operator.dash0.com/v1beta1/Dash0OperatorConfiguration` will not work.

## Uninstallation

To remove the Dash0 operator from your cluster, run the following command:

```console
helm uninstall dash0-operator --namespace dash0-system
```

Depending on the command you used to install the operator, you may need to use a different Helm release name or
namespace.

This will also automatically disable Dash0 monitoring for all namespaces by deleting the Dash0 monitoring resources in
all namespaces.
All workload modifications applied by the Dash0 operator will be reverted.
This will restart the pods of all workloads that were previously instrumented by the Dash0 operator.

Optionally, after `helm uninstall` has finished, remove the namespace that has been created for the operator:

```console
kubectl delete namespace dash0-system
```

If you choose to not remove the namespace, you might want to consider removing the secret with the Dash0 authorization
token (if such a secret has been created):

```console
kubectl delete secret --namespace dash0-system dash0-authorization-secret
```

If you later decide to install the operator again, you will need to perform the initial configuration steps again:

1. Set up a [Dash0 backend connection](configuration.md#configuring-the-dash0-backend-connection), and
2. Enable Dash0 monitoring in each namespace you want to monitor, see
   [Enable Dash0 Monitoring For a Namespace](configuration.md#enable-dash0-monitoring-for-a-namespace).

### Unsupported Uninstallation Procedures

> **Warning:** Do not use the following uninstallation procedures:

* Do not delete the Dash0 operator controller deployment manually, always use `helm uninstall` to remove the operator.
* Do not delete the Dash0 operator's namespace before running `helm uninstall` (this would also implicitly delete the
  operator deployment).

Deleting the Dash0 operator deployment without running `helm uninstall` will lead to an inconsistent state.
In particular, the operator's admission webhooks are still registered, but the service that responds to the webhook
requests has been removed, so all webhook requests will time out.
This will make requests to delete Dash0 monitoring resources fail.
In addition, the service that is responsible for removing the finalizer from the Dash0 monitoring resources is no longer
there.
In turn, this will make it harder to delete namespaces with a Dash0 monitoring resource, the namespace will get stuck in
the "Terminating" state, due to the finalizer in the monitoring resource no longer being handled correctly.

#### Recovery from Improper Uninstallation

To rectify an improper uninstallation, follow these steps:

1. Delete all Dash0 validating/mutating webhook configs manually (exact command depends on the Helm release name):
   ```console
   kubectl delete validatingwebhookconfiguration dash0-operator-monitoring-validator dash0-operator-operator-configuration-validator
   kubectl delete mutatingwebhookconfiguration dash0-operator-injector dash0-operator-monitoring-mutating dash0-operator-operator-configuration-mutating
   ```
2. Remove the finalizer from all Dash0 monitoring resources:
   ```console
   kubectl patch dash0monitorings <name> -n <namespace> --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'
   ```
3. Delete the Dash0 monitoring resources:
   ```console
   kubectl delete dash0monitorings <name> -n <namespace>
   ```

## Related Documentation

- [Installation](installation.md) - Initial installation instructions
- [Configuration](configuration.md) - Configuration options
- [Troubleshooting](troubleshooting.md) - Troubleshooting common issues
