# Debuginfo upload proxy

A standalone Parca gRPC proxy. With one upstream it forwards negotiation, completion, and streaming uploads without
changing protocol messages or upload IDs. With multiple upstreams it asks every destination whether an artifact is needed,
returns independent signed-URL upload instructions, and routes each completion to its originating destination. In that
mode, agents PUT the bytes directly to object storage; the proxy never fetches signed URLs or accepts streaming uploads.

The wire protocol is Parca's
[multi-backend upload protocol](https://github.com/parca-dev/parca/blob/main/pkg/debuginfo/MULTI_BACKEND_UPLOADS.md).
The Go module pins the generated bindings from the merged protocol change. No local protobuf fork is needed.

## Configuration

See [config.example.yaml](config.example.yaml) for a two-backend configuration. The shape follows Collector receivers,
exporters, and service pipelines, but this is a separate executable, not an OTLP receiver or a Collector pipeline component.
It uses the Collector's file and environment configuration providers and TLS configuration types. Unknown fields and
invalid pipeline references fail startup. Configuration is read once; restart the process after changing it.

```yaml
receivers:
  debuginfo:
    grpc:
      endpoint: 0.0.0.0:7070
      tls:
        cert_file: /tls/server.crt
        key_file: /tls/server.key

exporters:
  debuginfo/primary:
    endpoint: parca-primary:7070
    headers:
      authorization: Bearer ${env:PARCA_PRIMARY_TOKEN}
  debuginfo/secondary:
    endpoint: parca-secondary:7070
    headers:
      authorization: Bearer ${env:PARCA_SECONDARY_TOKEN}

service:
  pipelines:
    debuginfo:
      receivers: [debuginfo]
      exporters: [debuginfo/primary, debuginfo/secondary]
```

There is one `debuginfo` receiver and one `debuginfo` pipeline with 1 to 64 named `debuginfo/<name>` exporters.
One exporter automatically selects passthrough mode. Two or more select fanout mode, where every artifact goes to all
exporters listed in that pipeline. Exporter order does not affect completion routing. There is no routing configuration.

The receiver accepts requests without client credentials and is intended for agents on a trusted cluster network.
Only the proxy needs backend credentials. Incoming authorization and other metadata are not forwarded. Configure
downstream authentication and tenant-selection headers explicitly on each exporter. All agents use the same destinations
and exporter credentials. For isolated downstream tenants, deploy separate proxy configurations with separate credentials.
Restrict receiver access with network policies or an authenticated gateway; do not expose it directly to untrusted callers.

Exporter endpoints are `host:port`, without a URL scheme. Exporters use TLS with system roots by default. Collector TLS
options such as `ca_file`, `cert_file`, `key_file`, and `server_name_override` are available under `tls`. Certificate
verification cannot be disabled. Set `tls: {insecure: true}` only for plaintext development backends. Omitting receiver
`grpc.tls` enables plaintext inbound gRPC; use that only locally or behind a trusted TLS terminator. Restrict network access
to the proxy and protect bearer tokens with Kubernetes Secrets or equivalent secret storage.

Fanout uses fixed defaults: a 10-second negotiation/completion deadline, 8 concurrent downstream operations across all
requests, and a 15-minute completion-route lifetime. The deadline includes waiting for a concurrency slot; a shorter
client deadline takes precedence. Passthrough uses the caller's deadline without an additional proxy timeout. Inbound
messages are capped at 64 KiB in fanout mode and 64 MiB in passthrough mode to allow artifact chunks. The standard gRPC
health service reports process readiness, not the reachability of every backend.

## Completion routing and replicas

In passthrough mode, upload IDs remain unchanged. In fanout mode, the proxy wraps each upstream upload ID in a base64url
JSON route containing the stable exporter name, a fingerprint of its endpoint/TLS/headers configuration, the original
upload ID, the build ID, artifact type, and expiration. Completion restores the original ID and calls only that exporter.
Routes are encoded, not encrypted or authenticated: they are routing information, not authorization tokens. This relies
on the trusted-cluster network boundary. Routes can only select a configured exporter, never an arbitrary endpoint.
Signed URLs are returned byte-for-byte unchanged.

Any replica with the same configuration can complete a pending upload; no shared key is required. A nested fanout proxy
wraps the inner proxy's opaque upload ID and restores it before forwarding completion. Completion retries restore the same
downstream ID; the proxy never initiates another upload to recover a completion and relies on the backend's idempotent
completion contract.

Keep the mode, exporter names, and endpoint/TLS/headers configuration stable while uploads are outstanding. Reordering
exporters is safe. Removing, renaming or changing a fanout exporter produces an explicit error for its pending routes.
Changing credentials also changes its fingerprint. Do not repoint an endpoint to a different logical store while uploads
are pending. Drain outstanding uploads before changing configuration, or wait out the 15-minute route lifetime and any
backend upload lifetime. Upload IDs and signed URLs are not logged; do not add them to access logs outside this service.

## Failures and compatibility

With one upstream, legacy and multi-upload clients are supported. Requests, singular/list response shapes, reasons,
retry hints, raw upload IDs, forced uploads, and gRPC status errors pass through unchanged. Streaming `Upload` calls relay
artifact chunks to that upstream. Only configured exporter metadata is sent downstream, not incoming agent metadata.

With multiple upstreams:

- Clients must set `supports_multiple_uploads=true`. Legacy initiation is rejected with `FailedPrecondition` before any
  downstream calls. Clients must handle `retry_required`; preflight reasons include per-exporter diagnostics rather than
  preserving the exact legacy reason strings.
- Each initiation checks every destination again. An affirmative preflight remains useful if another destination fails.
  Successful targets are returned with `retry_required=true` when any destination remains unresolved.
- Negative temporary preflights preserve `retry_required`, including the old Parca upload-in-progress reason. If no
  destination says yes and a check fails, the proxy returns an RPC error rather than a definitive negative answer.
- A refused initiation with `FailedPrecondition` triggers a new preflight to distinguish a race from a persistent refusal.
  `AlreadyExists` remains an error with its diagnostic reason; it can mean rejection of the same invalid artifact hash.
- Singular instructions from older backends and lists from upgraded backends or nested proxies are accepted. Downstream
  retry hints are preserved. All-definitive-negative negotiation returns an empty list without a retry hint.
- gRPC byte-transfer instructions and unknown strategies are explicit configuration failures, not usable targets.
- `force=true` is rejected before contacting any backend. Stateless retry of a partial forced fanout could invalidate a
  live upload. Lost initiation responses can leave uploads pending until the backend's stale-upload timeout; subsequent
  negotiation preserves the retry hint rather than reporting delivery complete.
- There is no atomic multi-backend commit, artifact queue, or agent upload loop here. Agents must bound retries, retain
  unresolved artifacts, upload each target independently, and retry failed completions without re-uploading bytes.
  Permission/configuration errors require operator action; partial success does not guarantee eventual delivery.

## Build and test

From the repository root:

```sh
make debuginfo-proxy-unit-tests
make image-debuginfo-proxy
```

The image uses the `images/` build context, system CA certificates, and a scratch runtime as UID/GID 65532.
Mount the configuration and secret/certificate files read-only, then run:

```sh
/app/debuginfo-proxy --config=/etc/debuginfo-proxy/config.yaml
```

The tests exercise the public gRPC service over TCP and direct HTTP PUTs, mixed backend outcomes, legacy replies,
credential-free agent requests, exporter credentials without forwarding agent metadata, independent and repeated
completion, replica handoff after exporter reordering, malformed routes and artifact binding, expiration, replaced
destinations, nested collectors, fanout capability/force gating, deadlines, and single-upstream passthrough and streaming.
The operator does not deploy this component or modify its CRDs or Helm configuration.
