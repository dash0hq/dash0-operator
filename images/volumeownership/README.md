Volume Ownership
================

This image is attached as an init container to the OpenTelemetry collector pods, when a user-provided volume needs to be
made writable for the non-root collector process before the collector starts.

Unlike the `filelogoffsetvolumeownership` image (see `images/filelogoffsetvolumeownership/README.md`), this image is
generic: the directory (or directories) whose ownership should be changed are passed as command-line arguments instead
of being hardcoded in the entrypoint. This allows the same image to be reused for any collector volume mount, regardless
of the mount path. Each argument is treated as a directory path; the entrypoint creates it (if necessary) and hands its
file system permissions over to the user that runs the collector (65532:0).

The collector process runs as the non-root user 65532:0. It needs the mounted volume directories to be owned by that
user, so that it can create files in them.

Depending on which user/group owns a freshly mounted directory and which permissions it has, this might or might not be
the case for the user 65532:0.
For hostPath volumes of type DirectoryOrCreate, it is usually not the case, because they are owned by root by default
and are not group-writable.
There are other volume types where this condition also does not hold, for example when using a simple
PersistentVolumeClaim like the following on GKE Autopilot:
```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: some-claim
  namespace: operator-namespace
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 300Mi
  storageClassName: standard-rwo
```

Failure to set the ownership correctly will result in the mount directory being owned by `root:root`, which in turn will
result in a CrashLoopBackOff for the DaemonSet collector's `opentelemetry-collector` container, because it cannot create
files in the mounted directory.

For this reason, we attach this init container, running as root, and executing the following actions for each directory
passed as an argument:
- create the required directory ahead of time (usually a no-op since the directory _is_ the mount point),
- then hand over file system permissions to 65532:0 for the directory.

To complicate matters more, we also need to accomodate systems where calling chown (even for root) is rejected with
"Operation not permitted". One example are AWS EFS-backed PVC volumes with dynamic provisioning (see
https://github.com/kubernetes-sigs/aws-efs-csi-driver/tree/master/examples/kubernetes/dynamic_provisioning).
The uid/gid is defined by EFS, and cannot be changed, not even by root.
Conveniently, the OpenTelemetry collector process can still write to the directory, even though it should not be able
to according to the directories ownership (something like `50000:50000`, so a different uid and gid than the collector
process) and permissions (`drwxr-xr-x`, i.e. neither world- nor group-writable).

Notably, so far we have not seen cases where root cannot execute chown _and_ the actual/effective file system
permissions prohibit the collector from writing to the directory.

Hence, the approach is to *attempt* to execute chown, but do not let the init container fail with an error if it is not
successful.
This is why the entrypoint.sh script deliberately runs without set -e.
The additional `ls` commands serve to have at least a minimum of troubleshooting information in case we encounter setups
where the current approach might not be adequate.
