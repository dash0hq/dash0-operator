Filelog Receiver Offset Synchronization
=======================================

See opentelemetry-collector-contrib/receiver/filelogreceiver/README.md#offset-tracking.

This container is added to the daemonset OTel collector pod, to keep track of log file offsets.
Its main purpose is to ensure that log records are not emitted multiple times by the file_log receiver when the collector
pod is restarted.

*Note:* By default, the operator stores the offsets in a `hostPath` volume on each node, if the cluster admits a
collector pod with a writable `hostPath` volume (see `resolveFilelogOffsetStorage` in
`internal/collectors/otelcolresources/filelog_offset_storage.go`).
In that case, and if a user-provided volume is configured via the Helm chart property
`operator.collectors.filelogOffsetSyncStorageVolume`, the log file offset information is directly stored on the volume,
and the sync container and the init container described below are not used, except for migrating
existing offsets from the config map to the volume, see the last section.
The config map based storage described here is the fallback for clusters that do not admit writable `hostPath`
volumes (for example GKE Autopilot, OpenShift, or clusters with admission policies that reject them), and can be
requested explicitly via `operator.collectors.filelogOffsetStorageMode=configMap`.

The remainder of this document assumes that the config map based storage is used.

Since the whole mechanism requires a couple of components to work together, here is a description of how everything is
connected:

- The configuration for the file_log receiver (see `daemonset.config.yaml.template`) sets the property
  `storage: file_storage/filelogreceiver_offsets` and also defines a file storage extension with the same name,
  backed by the directory `/var/otelcol/filelogreceiver_offsets`.
- This directory in turn is backed by a volume mount (`desired_state.go#filelogReceiverOffsetsVolumeMount`), which
  is added to the daemonset collector container, the filelog offset sync container and additional the filelog offset
  sync init container (which all belong to the daemonset collector pod).
- The volume mounts all refer to the volume named `filelogreceiver-offsets` of that pod
  (`desired_state.go#assembleCollectorDaemonSetVolumes`), which is an `EmptyDir` type volume with a limit of 10 MB.
- This way, the filelog offset sync container as well as the filelog offset sync init container have access to the
  filelog offset files which the collector container's file_log receiver component writes.

The `EmptyDir` volume only exists for the lifetime of the pod.
The purpose of storing the offsets in the first place is to have them available across pod restarts, so we need to
synchronize them in some kind of permanent storage after the file_log receiver writes them to the
`filelogreceiver-offsets` volume.
For that purpose, the operator creates a config map named `${helm release name}-filelogoffsets-cm` (see `desired_state.go#assembleFilelogOffsetsConfigMap`).

The `filelog-offset-sync` container regularly synchronizes the offsets from the `filelogreceiver-offsets` volume
to the `${helm release name}-filelogoffsets-cm` config map.
This mode is activated by starting with `--mode=sync`.

The `filelog-offset-init` container restores the offsets  from a previously running collector pod from that config map
at startup and populates the `filelogreceiver-offsets` volume with the restored offset files.
This mode is activated by starting with `--mode=init`.

Note that config maps are limited to 1 MB in size.
For clusters with a lot of pods, we might run into this limit.
Please use `operator.collectors.filelogOffsetSyncStorageVolume` for those clusters, see
https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/docs/advanced-configuration.md#providing-a-filelog-offset-volume.

Migrating Offsets from the Config Map to a Volume
-------------------------------------------------

When the collector daemonset stores the offsets on a volume (the `hostPath` volume chosen by the operator, or a
user-provided volume) while the offsets config map of the config map based storage still exists (for example after
upgrading from an operator version that only had the config map based storage), the collector pods get a
`filelog-offset-migrate` init container (`--mode=migrate`).
It runs after the volume ownership init container and restores the offsets of its node from the config map into the
volume, like `--mode=init`, but only if the offset directory on the volume is still empty, so it never overwrites
offsets that a collector has already written to the volume.
This way, the collector continues where it left off, instead of losing or re-sending log records after the switch.
The operator deletes the config map once the daemonset has been rolled out completely with the volume.
From then on, the migrate container finds no config map and does nothing.

The migration is crash-safe, so that an interrupted attempt (disk full, OOM kill, pod deletion) never leaves the
collector with a partially written offset file:
- The offset files are restored into the staging directory `.filelogoffsetsync-migrate-staging` inside the offset
  directory and synced to disk.
  The volume is mounted with a per-node `subPath`, so the offset directory is the only directory on that file system
  the container can access.
- The staging directory is then renamed to `.filelogoffsetsync-migrate-committed`.
- Only after that are the files moved to their final paths in the offset directory, each with a single rename, and
  the committed directory is removed.

On start, the migrate container discards a staging directory left over by an earlier attempt and finishes moving the
files of a leftover committed directory.
It ignores both directories when checking whether the offset directory is empty, so an attempt that failed before
committing is simply repeated.

Errors reading the config map from the Kubernetes API and errors writing to the volume (for example a full disk or
missing permissions) make the migrate container fail, so that it is retried.
Offset files in the config map that cannot be restored, because the node's archive is corrupt or contains no offset
files, would fail on every retry and keep the collector from starting on that node.
Instead, the migrate container logs an error naming the config map and the node, discards what it has restored so far,
and exits successfully.
The collector then starts without these offsets, as it would on a new node, so log records written while switching to
the volume may be missed or emitted again.
