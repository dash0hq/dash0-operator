# Dash0 Kubernetes Operator Helm Chart

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/dash0-operator)](https://artifacthub.io/packages/search?repo=dash0-operator)

This repository contains the [Helm](https://helm.sh/) chart for the Dash0 Kubernetes operator.

The Dash0 Kubernetes Operator makes observability for Kubernetes _easy_.
Simply install the operator into your cluster to get OpenTelemetry data flowing from your Kubernetes workloads to Dash0.

## Description

The Dash0 Kubernetes operator installs an OpenTelemetry collector into your cluster that sends data to your Dash0
ingress endpoint, with authentication already configured out of the box. Additionally, it will enable gathering
OpenTelemetry data from applications deployed to the cluster for a selection of supported runtimes.

More information on the Dash0 Kubernetes Operator can be found at
https://github.com/dash0hq/dash0-operator/blob/main/README.md.

## Prerequisites

- [Kubernetes](https://kubernetes.io/) >= 1.xx
- [Helm](https://helm.sh) >= 3.xx, please refer to Helm's [documentation](https://helm.sh/docs/) for more information
  on installing Helm.

## Installation

Before installing the operator, add the Dash0 operator's Helm repository as follows:

```console
helm repo add dash0-operator https://dash0hq.github.io/dash0-operator
helm repo update
```

Now you can install the operator into your cluster via Helm with the following command:

```console
helm install \
  --namespace dash0-system \
  --create-namespace \
  dash0-operator \
  dash0-operator/dash0-operator
```

Note that Dash0 has to be enabled per namespace that you want to monitor, which is described in the next section.

## Enable Dash0 Monitoring For a Namespace

For _each namespace_ that you want to monitor with Dash0, enable monitoring by installing a Dash0 monitoring resource
into that namespace:

Create a file `dash0-monitoring.yaml` with the following content:
```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0
metadata:
  name: dash0-monitoring-resource
spec:
  # Replace this value with the actual OTLP/gRPC endpoint of your Dash0 organization.
  ingressEndpoint: ingress... # TODO needs to be replaced with the actual value, see below

  # Either provide the Dash0 authorization token as a string via the property authorizationToken:
  authorizationToken: auth_... # TODO needs to be replaced with the actual value, see below

  # Opt-out settings for particular use cases. The default value is "all". Other possible values are
  # "created-and-updated" and "none".
  # instrumentWorkloads: all

  # Opt-out setting for removing instrumentation from workloads when the Dash0 monitoring resource is removed.
  # uninstrumentWorkloadsOnDelete: true
```

At this point, you need to provide two configuration settings:
* `ingressEndpoint`: The URL of the observability backend to which telemetry data will be sent. This property is
  mandatory.
  Replace the value in the example above with the OTLP/gRPC endpoint of your Dash0 organization.
  The correct OTLP/gRPC endpoint can be copied fom https://app.dash0.com/settings.
  Note that the correct endpoint value will always start with `ingress.` and end in `dash0.com:4317`.
  A protocol prefix (eg. `https://`) should not be included in the value.
* `authorizationToken` or `secretRef`: Exactly one of these two properties needs to be provided.
  If both are provided, `authorizationToken` will be used and `secretRef` will be ignored.
    * `authorizationToken`: Replace the value in the example above with the Dash0 authorization token of your
      organization.
      The authorization token for your Dash0 organization can be copied from https://app.dash0.com/settings.
      The prefix `Bearer ` must *not* be included in the value.
      Note that the value will be rendered verbatim into a Kubernetes ConfigMap object.
      Anyone with API access to the Kubernetes cluster will be able to read the value.
      Use the `secretRef` property and a Kubernetes secret if you want to avoid that.
    * `secretRef`: Replace the value in the example above with the name of an existing Kubernetes secret in the Dash0
       operator's namespace.
       The secret needs to contain the Dash0 authorization token.
       See below for details on how exactly the secret should be created.
       Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes
       cluster will be able to read the value.
       Additional steps are required to make sure secret values are encrypted.
       See https://kubernetes.io/docs/concepts/configuration/secret/ for more information on Kubernetes secrets.

The other configuration settings are optional:
* `instrumentWorkloads`: A global opt-out for workload instrumentation for the target namespace.
  There are threepossible settings: `all`, `created-and-updated` and `none`.
  By default, the setting `all` is assumed.

  * `all`: If set to `all` (or omitted), the operator will:
      * instrument existing workloads in the target namespace (i.e. workloads already running in the namespace) when
        the Dash0 monitoring resource is deployed,
      * instrument existing workloads or update the instrumentation of already instrumented workloads in the target
        namespace when the Dash0 Kubernetes operator is first started or restarted (for example when updating the
        operator),
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
  Note that the first two actions (instrumenting existing workloads) will result in restarting the pods of the
  affected workloads.

  * `created-and-updated`: If set to `created-and-updated`, the operator will not instrument existing workloads in the
    target namespace.
    Instead, it will only:
      * instrument new workloads in the target namespace when they are deployed, and
      * instrument changed workloads in the target namespace when changes are applied to them.
        This setting is useful if you want to avoid pod restarts as a side effect of deploying the Dash0 monitoring
        resource or restarting the Dash0 Kubernetes operator.

  * `none`: You can opt out of instrumenting workloads entirely by setting this option to `none`.
     With `instrumentWorkloads: none`, workloads in the target namespace will never be instrumented to send telemetry to
     Dash0.

  If this setting is omitted, the value `all` is assumed and new/updated as well as existing Kubernetes workloads
  will be intrumented by the operator to send telemetry to Dash0, as described above.

  More fine-grained per-workload control over instrumentation is available by setting the label
  `dash0.com/enable=false` on individual workloads.

* `uninstrumentWorkloadsOnDelete`: A boolean opt-out setting for removing the Dash0 instrumentation from workloads when
  the Dash0 monitoring resource is removed from a namespace, or when the Dash0 Kubernetes operator is deleted entirely.
  By default, this setting is true and the operator will revert the instrumentation modifications it applied to
  workloads to send telemetry to Dash0.
  Setting this option to `false` will prevent this behavior.
  Note that removing instrumentation will typically result in a restart of the pods of the affected workloads.

  The default value for this option is true.

After providing the required values, save the file and apply the resource to the namespace you want to monitor.
For example, if you want to monitor workloads in the namespace `my-nodejs-applications`, use the following command:

```console
kubectl apply --namespace my-nodejs-applications -f dash0-monitoring.yaml
```

If you want to monitor the `default` namespace with Dash0, use the following command:
```console
kubectl apply -f dash0-monitoring.yaml
```

### Using a Kubernetes Secret for the Dash0 Authorization Token

If you want to provide the Dash0 authorization token via a Kubernetes secret instead of providing the token as a string,
create the secret in the namespace where the Dash0 operator is installed.
If you followed the guide above, the name of that namespace is `dash0-system`.
The authorization token for your Dash0 organization can be copied from https://app.dash0.com/settings.
You can freely choose the name of the secret.
Make sure to use `dash0-authorization-token` as the token key.

Create the secret by using the following command:

```console
kubectl create secret generic \
  dash0-authorization-secret \
  --namespace dash0-system \
  --from-literal=dash0-authorization-token=auth_...your-token-here...
```

With this example command, you would create a secret with the name `dash0-authorization-secret` in the namespace
`dash0-system`.
If you installed the operator into a different namespace, replace the `--namespace` parameter accordingly.

The name of the secret must be referenced in the YAML file for the Dash0 monitoring resource in the `secretRef` property.
Here is an example that uses the secret created above:
```yaml
apiVersion: operator.dash0.com/v1alpha1
kind: Dash0Monitoring
metadata:
  name: dash0-monitoring-resource
spec:
  # Replace this value with the actual OTLP/gRPC endpoint of your Dash0 organization.
  ingressEndpoint: ingress... # TODO needs to be replaced with the actual value, see below

  # Or provide the name of a secret existing in the Dash0 operator's namespace as the property secretRef:
  secretRef: dash0-authorization-secret
```

Note that by default, Kubernetes secrets are stored _unencrypted_, and anyone with API access to the Kubernetes cluster
will be able to read the value.
Additional steps are required to make sure secret values are encrypted.
See https://kubernetes.io/docs/concepts/configur**ation/secret/ for more information on Kubernetes secrets.

## Disable Dash0 Monitoring For a Namespace

If you want to stop monitoring a namespace with Dash0, remove the Dash0 monitoring resource from that namespace.
For example, if you want to stop monitoring workloads in the namespace `my-nodejs-applications`, use the following
command:

```console
kubectl delete --namespace my-nodejs-applications Dash0Monitoring dash0-monitoring-resource
```

or, alternatively, by using the `dash0-monitoring.yaml` file created earlier:

```console
kubectl delete --namespace my-nodejs-applications -f dash0-monitoring.yaml
```

## Uninstallation

To remove the Dash0 Kubernetes Operator from your cluster, run the following command:

```
helm uninstall dash0-operator --namespace dash0-system
```

Depending on the command you used to install the operator, you may need to use a different Helm release name or
namespace.

This will also automatically disable Dash0 monitoring for all namespaces by deleting the Dash0 monitoring resources in all
namespaces.
Optionally, remove the namespace that has been created for the operator:

```
kubectl delete namespace dash0-system
```

If you choose to not remove the namespace, you might want to consider removing the secret with the Dash0 authorization
token (if such a secret has been created):

```console
kubectl delete secret --namespace dash0-system dash0-authorization-secret
```

If you later decide to install the operator again, you will need to enable Dash0 monitoring in each namespace you want
to monitor again, see [Enable Dash0 Monitoring For a Namespace](#enable-dash0-monitoring-for-a-namespace).
