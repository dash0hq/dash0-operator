Filelog Offset Volume Ownership
===============================

This image is attached as an init container to the OpenTelemetry collector DaemonSet pods, when a volume is used to
store filelog offsets for the filelog receiver.
The user-provided volume is mounted at /var/otelcol/filelogreceiver_offsets, both for the collector containerand also
for this init container.

We use the file_storage extension to store log file offsets. This is configured in daemonset.config.yaml.template
in the section extensions.file_storage/filelogreceiver_offsets.
The extension is attached to receivers.filelog via the storage attribute.
The storage directory configured for file_storage/filelogreceiver is /var/otelcol/filelogreceiver_offsets, that is,
the extension uses the volume mount's directory.

We need to make sure that this directory /var/otelcol/filelogreceiver_offsets is owned by the user that runs the
collector (65532:0), so that the file_storage extension can write to this directory.
In particular, it needs to be able to create files in that directory.

Depending on which users/group owns the directory and which permissions it has, this might or might not be the case for
the user 65532:0.
For hostPath volumes of type DirectoryOrCreate, it is usually not the case, because they are owned by root by default
and are not group-writable.
There are other volume types where this condition also does not hold, for example when using a simple
PersistentVolumeClaim like the following on GKE Autopilot:
```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: offset-storage-claim
  namespace: operator-namespace
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 300Mi
  storageClassName: standard-rwo
```

Failure to set the ownership correctly will result in `/var/otelcol/filelogreceiver_offsets` to be owned by `root:root`,
which in turn will result in a CrashLoopBackOff for the DaemonSet collector's `opentelemetry-collector` container:
```
opentelemetry-collector Error: cannot start pipelines: failed to start "filelog" receiver: storage client: open /var/otelcol/filelogreceiver_offsets/receiver_filelog_: permission denied
opentelemetry-collector 2025/11/19 14:56:38 collector server run finished with error: cannot start pipelines: failed to start "filelog" receiver: storage client: open /var/otelcol/filelogreceiver_offsets/receiver_filelog_: permission denied
```

For this reason, we attach this init container, running as root, and executing the following actions:
- create the required directory ahead of time (usually a no-op since the directory _is_ the mount point),
- then hand over file system permissions to 65532:0 for /var/otelcol/filelogreceiver_offsets.

To complicate matters more, we also need to accomodate systems where calling chown (even for root) is rejected with
"Operation not permitted". One example are AWS EFS-backed PVC volumes with dynamic provisioning (see
https://github.com/kubernetes-sigs/aws-efs-csi-driver/tree/master/examples/kubernetes/dynamic_provisioning).
The uid/gid is defined by EFS, and cannot be changed, not even by root.
Conveniently, the OpenTelemetry collector process can still write to the directory, even though it should not be able
to according to the directories ownership (something like `50000:50000`, so a different uid and gid than the collector
process) and permissions (`drwxr-xr-x`, i.e. neither world- nor group-writable).

Notably, so far we have not seen cases where root cannot execute chown _and_ the actual/effective file system
permissions prohibit the file_storage extension from write to the directory.

Hence, the approach is to *attempt* to execute chown, but do not let the init container fail with an error if it is not
successful.
This is why the entrypoint.sh script deliberately runs without set -e.
The additional `ls` commands serve to have at least a minimum of troubleshooting information in case we encounter setups
where the current approach might not be adequate.
