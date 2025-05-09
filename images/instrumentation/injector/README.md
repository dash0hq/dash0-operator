## Dash0 Injector

The Dash0 injector is a small library (written in [Zig](https://ziglang.org/)) that is intended to be used via
`LD_PRELOAD` to inject environment variables into processes.

It serves two main purposes:
* Inject an OpenTelemetry SDK/distribution into the process to enable tracing for supported runtimes.
* Set resource attributes automatically, in particular Kubernetes related resource attributes and service related
  resource attributes.

The injector is used by the Dash0 operator to enable automatic zero-touch instrumenation of Kubernetes workloads.
In particular, workloads in namespaces monitored by the Dash0 operator are modified by the Dash0 operator
to enable tracing for supported runtimes out of the box, and to improve Kubernetes-related resource attribute
auto-detection.

The Dash0 instrumentation image (being added as an init container) adds the Dash0 injector shared object and the
required OpenTelemetry SDKs and distributions to the target container's file system at container startup.
Also, the injector is added to processes at startup via an `LD_PRELOAD` hook.
For that purpose, the Dash0 operator adds the `LD_PRELOAD` environment variable to the pod spec template of the workload.
`LD_PRELOAD` is an environment variable that is evaluated by the
[dynamic linker/loader](https://man7.org/linux/man-pages/man8/ld.so.8.html) when a Linux executable starts.
It specifies a list of additional shared objects to be loaded before the actual code of the executable.
At process startup the Dash0 injector code is loaded first and adds additional environment variables (or modifies
existing ones) to the running process by hooking into the `getenv` function of the standard library.

* It sets (or appends to) `NODE_OPTIONS` to activate the [Dash0 OpenTelemetry distribution for Node.js](https://github.com/dash0hq/opentelemetry-js-distribution) to collect tracing data from all Node.js workloads.
* It adds a `-javaagent` flag to `JAVA_TOOL_OPTIONS` to activate the Java OTel SDK.
* It inspects specific existing environment variables and populates `OTEL_RESOURCE_ATTRIBUTES` with additional resource
  attributes. These environment variables are usually set by the Dash0 operator on the pod spec template of the
  workload. If `OTEL_RESOURCE_ATTRIBUTES` is already set, the additional key-value pairs are prepended to the
  existing value of `OTEL_RESOURCE_ATTRIBUTES`. The following environment variables and resource attributes are
  supported:
    * `DASH0_NAMESPACE_NAME` will be translated to `k8s.namespace.name`
    * `DASH0_POD_NAME` will be translated to `k8s.pod.name`
    * `DASH0_POD_UID` will be translated to `k8s.pod.uid`
    * `DASH0_CONTAINER_NAME` will be translated to `k8s.container.name`
    * `DASH0_SERVICE_NAME` will be translated to `service.name`
    * `DASH0_SERVICE_VERSION` will be translated to `service.version`
    * `DASH0_SERVICE_NAMESPACE` will be translated to `service.namespace`
    * `DASH0_RESOURCE_ATTRIBUTES` is expected to contain key-value pairs
      (e.g. `my.resource.attribute=value,my.other.resource.attribute=another-value`) and will be added as-is.

## But Why?

Why go to such length to set all these variables within the running proces instead of just letting the Dash0 operator
set them directly on the pod spec template?
That is because not all environment variables that a workload will eventually use are even visible on the Kubernetes
level.
Naturally, if for example `OTEL_RESOURCE_ATTRIBUTES` or `NODE_OPTIONS` are present on the original pod spec template,
and are not modified later by other means (see below), the operator could manage all necessary modifications much more
simply by modifying the pod spec template.

But workloads can (and do!) also set environment variables via other means, for example by
* an `ENV` instruction in the Dockerfile, or
* setting environment variables in the container's entrypoint script.

Environment variables added via mechaninsms like that are not visible on the Kubernetes level, hence purely relying on
Kubernetes level pod spec template modifications is bound to run into conflicts and behaviour that is hard to reason
about, and even harder to troubleshoot.
That is the reason we go the extra mile and inspect the environment at process runtime and apply the necessary
modifications at that point, when we have the full picture of all environment variables that are actually available in
the process.