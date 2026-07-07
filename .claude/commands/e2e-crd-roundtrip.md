# End-to-End CRD Roundtrip Tests

Run end-to-end roundtrip tests for Dash0 API-synced CRDs against a real Dash0 backend. Tests the full lifecycle: create, verify sync status in K8s, verify via Dash0 CLI, update, re-verify, delete, and confirm deletion from the backend.

The CRD type to test is specified as $ARGUMENTS (e.g., `spam-filters`, `views`, `synthetic-checks`, `notification-channels`, `sampling-rules`). If not specified, ask the user which CRD type to test.

## Supported CRD types

| CLI argument | K8s Kind | API Version | K8s plural | Dash0 CLI command | Namespaced |
|---|---|---|---|---|---|
| `spam-filters` | `Dash0SpamFilter` | `v1alpha1` | `dash0spamfilters` | `dash0 --experimental spam-filters` | Yes |
| `views` | `Dash0View` | `v1alpha1` | `dash0views` | `dash0 views` | Yes |
| `synthetic-checks` | `Dash0SyntheticCheck` | `v1alpha1` | `dash0syntheticchecks` | `dash0 synthetic-checks` | Yes |
| `notification-channels` | `Dash0NotificationChannel` | `v1beta1` | `dash0notificationchannels` | `dash0 notification-channels` | Yes |
| `sampling-rules` | `Dash0SamplingRule` | `v1alpha1` | `dash0samplingrules` | `dash0 sampling-rules` | Yes |

Some CLI subcommands require `--experimental` (e.g., spam-filters). Check via `dash0 --agent-mode help` if unsure.

## Prerequisites

- A kind cluster with the operator deployed and running (use `/setup-test-cluster` if needed).
- The Dash0 CLI (`dash0`) installed and configured with API access and auth token.
- A `Dash0OperatorConfiguration` resource with a Dash0 export including `apiEndpoint` and `authorization.token`.
- A test namespace with a `Dash0Monitoring` resource.

### Setting up prerequisites (if not present)

1. **Check operator is running:**
   ```bash
   kubectl get pods -n dash0-system
   ```

2. **Deploy operator if needed** (see `/setup-test-cluster` skill for full details):
   ```bash
   helm upgrade --install --namespace dash0-system --create-namespace \
     --set operator.image.repository=operator-controller --set operator.image.tag=latest --set operator.image.pullPolicy=Never \
     --set operator.initContainerImage.repository=instrumentation --set operator.initContainerImage.tag=latest --set operator.initContainerImage.pullPolicy=Never \
     --set operator.collectorImage.repository=collector --set operator.collectorImage.tag=latest --set operator.collectorImage.pullPolicy=Never \
     --set operator.configurationReloaderImage.repository=configuration-reloader --set operator.configurationReloaderImage.tag=latest --set operator.configurationReloaderImage.pullPolicy=Never \
     --set operator.filelogOffsetSyncImage.repository=filelog-offset-sync --set operator.filelogOffsetSyncImage.tag=latest --set operator.filelogOffsetSyncImage.pullPolicy=Never \
     --set operator.filelogOffsetVolumeOwnershipImage.repository=filelog-offset-volume-ownership --set operator.filelogOffsetVolumeOwnershipImage.tag=latest --set operator.filelogOffsetVolumeOwnershipImage.pullPolicy=Never \
     --set operator.targetAllocatorImage.repository=target-allocator --set operator.targetAllocatorImage.tag=latest --set operator.targetAllocatorImage.pullPolicy=Never \
     --set operator.developmentMode=true \
     --wait --timeout 120s \
     dash0-operator helm-chart/dash0-operator
   ```

3. **Create operator configuration** — get credentials from CLI config (`dash0 --agent-mode config show`):
   ```yaml
   apiVersion: operator.dash0.com/v1alpha1
   kind: Dash0OperatorConfiguration
   metadata:
     name: dash0-operator-configuration
   spec:
     exports:
       - dash0:
           endpoint: <INGRESS_ENDPOINT>
           apiEndpoint: <API_ENDPOINT>
           dataset: <DATASET>
           authorization:
             token: <AUTH_TOKEN>
   ```

4. **Create test namespace and monitoring resource:**
   ```bash
   kubectl create namespace test-e2e-crd
   sleep 5
   kubectl apply -n test-e2e-crd -f - <<EOF
   apiVersion: operator.dash0.com/v1beta1
   kind: Dash0Monitoring
   metadata:
     name: dash0-monitoring
     namespace: test-e2e-crd
   spec: {}
   EOF
   ```

   **Troubleshooting:** If the monitoring resource creation fails:
   - "no Dash0 operator configuration resources are available": wait a few seconds and retry.
   - TLS/certificate error: restart the operator pod and wait for it to be ready (`kubectl delete pod -n dash0-system -l app.kubernetes.io/name=dash0-operator`), then `sleep 10` before retrying.

## Test Execution

### Phase 1: Record baseline

Record the initial state from the Dash0 backend:

```bash
dash0 --agent-mode [--experimental] <crd-cli-command> list --dataset <DATASET>
```

Count how many resources exist with `dash0.com/source: operator`. This is the baseline.

### Phase 2: Generate and create test scenarios

Generate at least 10 diverse test scenarios for the CRD type. Scenarios should cover:

- **Minimal spec**: The simplest valid resource
- **All optional fields populated**: A resource with every optional field set
- **Variations of enum/list fields**: Different values for fields that accept multiple options (e.g., different contexts for spam filters, different check plugin kinds for synthetic checks)
- **Multiple sub-items**: Resources with multiple filter conditions, multiple assertions, etc.
- **Edge cases**: Long names, special characters in values (within K8s naming rules), maximum array sizes

Use the sample YAMLs in `test-resources/customresources/` as a starting point for realistic specs.

Apply all scenarios in a single `kubectl apply -n test-e2e-crd -f -`.

### Phase 3: Verify creation (K8s status)

Wait ~15 seconds for reconciliation, then check sync status:

```bash
kubectl get <k8s-plural> -n test-e2e-crd \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.synchronizationStatus}{"\t"}{.status.synchronizedAt}{"\n"}{end}'
```

**Expected:** All resources show `synchronizationStatus: successful`.

If any show empty status, check operator logs for errors:
```bash
kubectl logs -n dash0-system deploy/dash0-operator-controller --tail=50 | grep -i error
```

### Phase 4: Verify creation (Dash0 CLI)

List all resources via CLI and validate each scenario:

```bash
dash0 --agent-mode [--experimental] <crd-cli-command> list --dataset <DATASET>
```

For each scenario, verify:
1. It exists in the CLI output with `dash0.com/source: operator`
2. The `name` matches the K8s resource name
3. The spec fields match what was submitted

Write a validation script that programmatically checks all scenarios and prints a pass/fail table.

### Phase 5: Update test

Pick one scenario and modify its spec meaningfully (e.g., add/remove fields, change values). Apply the change via `kubectl apply`.

Wait ~10 seconds, then verify:
1. K8s status shows `successful` with an updated `synchronizedAt` timestamp
2. The `dash0.com/id` in the status remains the same (update, not recreate)
3. The Dash0 CLI `get <id>` shows the updated spec

### Phase 6: Delete and verify cleanup

Delete all test resources:

```bash
kubectl delete <k8s-plural> --all -n test-e2e-crd
```

Wait ~10 seconds, then verify via CLI that operator-managed resources are back to the baseline count from Phase 1.

## Pass/Fail Criteria

The test suite **passes** if:
- All scenarios sync to `successful` status in K8s
- All scenarios appear in the Dash0 CLI with correct spec and `source: operator`
- The update test preserves the `dash0.com/id` and reflects the new spec
- After deletion, all test resources are removed from the backend (operator-managed count returns to baseline)

The test suite **fails** if:
- Any scenario has `synchronizationStatus` other than `successful`
- Any scenario's spec in the CLI doesn't match what was submitted
- The update creates a new ID instead of preserving the existing one
- Any test resource remains in the backend after deletion

## Cleanup

After the test, clean up:

```bash
kubectl delete <k8s-plural> --all -n test-e2e-crd
kubectl delete dash0monitoring dash0-monitoring -n test-e2e-crd
kubectl delete namespace test-e2e-crd
```

Full teardown (optional):

```bash
kubectl delete dash0operatorconfiguration dash0-operator-configuration
helm uninstall --namespace dash0-system dash0-operator
kind delete cluster --name <cluster-name>
```
