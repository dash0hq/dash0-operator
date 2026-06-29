# Troubleshooting

This guide covers common troubleshooting procedures for the Dash0 operator.

## Table of Contents

- [Create Heap Profiles](#create-heap-profiles)
  - [Operator Manager Heap Profile](#operator-manager-heap-profile)
  - [Collector Daemonset Heap Profile](#collector-daemonset-heap-profile)
  - [Collector Deployment Heap Profile](#collector-deployment-heap-profile)

## Create Heap Profiles

The instructions in this section are mainly meant to be used in a shared troubleshooting session with Dash0 support.

### Operator Manager Heap Profile

To get a heap profile from the operator manager container:

1. Deploy the operator manager with the additional Helm value `operator.pprofPort=1777`.
2. Take note of the namespace the operator is deployed in (default: `dash0-system`).
3. Run `kubectl get pod -n <operator-namespace> -l app.kubernetes.io/component=controller` to get the name of the
   operator manager pod (usually something like `dash0-operator-controller-xxxxxxxxx-xxxxx`, but the name depends on the
   Helm release name).
4. Using the information from the previous two steps, run
   `kubectl port-forward -n <operator-namespace> <operator-manager-pod-name> 1777`.
5. In a separate shell, while the `kubectl port-forward` command from the previous step is still running, run
   `curl http://localhost:1777/debug/pprof/heap > dash0-operator-manager-heap.out`.
6. Terminate the `kubectl port-forward` command.
7. Redeploy the operator without the Helm setting `operator.pprofPort=1777`.

### Collector Daemonset Heap Profile

To get a heap profile from a OpenTelemetry collector daemonset container:

1. Deploy the operator manager with the additional Helm value `operator.collectors.enablePprofExtension=true`.
2. Take note of the namespace the operator is deployed in (default: `dash0-system`).
3. Run `kubectl top pod -n <operator-namespace> -l app.kubernetes.io/component=agent-collector` to get the name of a
   collector pod that has high memory usage (usually something like
   `dash0-operator-opentelemetry-collector-agent-daemonset-xxxxx`, but the name depends on the Helm release name).
4. Using the information from the previous two steps, run
   `kubectl port-forward -n <operator-namespace> <collector-daemonset-pod-name> 1777`.
5. In a separate shell, while the `kubectl port-forward` command from the previous step is still running, run
   `curl http://localhost:1777/debug/pprof/heap > dash0-daemonset-collector.out`.
6. Terminate the `kubectl port-forward` command.
7. Redeploy the operator without the Helm setting `operator.collectors.enablePprofExtension=true`.

### Collector Deployment Heap Profile

To get a heap profile from a OpenTelemetry collector deployment container:

* Follow the same steps as for the collector daemonset, but use
  `-l app.kubernetes.io/component=cluster-metrics-collector` in step (3).

## Getting Help

For additional help:
- Review the operator logs: `kubectl logs -n dash0-system -l app.kubernetes.io/component=controller`
- Contact Dash0 support at support@dash0.com

## Related Documentation

- [Platform Specific](platform-specific.md) - Platform-specific notes that may help troubleshoot issues
- [Configuration](configuration.md) - Verify your configuration settings
