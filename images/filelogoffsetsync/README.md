Filelog Receiver Offset Synchronization
=======================================

See opentelemetry-collector-contrib/receiver/filelogreceiver/README.md#offset-tracking.

This container is added to the daemonset OTel collector pod, to keep track of log file offsets.
Its main purpose is to ensure that log records are not emitted multiple times by the filelog receiver when the collector
pod is restarted.

*Note:* If a user-provided volume is configured via the Helm chart property
`operator.collectors.filelogOffsetSyncStorageVolume`), this container image is not used.
Instead, the log file offset information is directly stored on the user-provided volume.
The purpose of this container image is to improve the out-of-the-box experience for users with small- to medium-sized
clusters, as they do not need to create or configure a volume.

The remainder of this document assumes that the user-provided volume has not been set, and offsets are persisted with
the help of this container image.

Since the whole mechanism requires a couple of components to work together, here is a description of how everything is
connected:

- The configuration for the filelog receiver (see `daemonset.config.yaml.template`) sets the property
  `storage: file_storage/filelogreceiver_offsets` and also defines a file storage extension with the same name,
  backed by the directory `/var/otelcol/filelogreceiver_offsets`.
- This directory in turn is backed by a volume mount (`desired_state.go#filelogReceiverOffsetsVolumeMount`), which
  is added to the daemonset collector container, the filelog offset sync container and additional the filelog offset
  sync init container (which all belong to the daemonset collector pod).
- The volume mounts all refer to the volume named `filelogreceiver-offsets` of that pod
  (`desired_state.go#assembleCollectorDaemonSetVolumes`), which is an `EmptyDir` type volume with a limit of 10 MB.
- This way, the filelog offset sync container as well as the filelog offset sync init container have access to the
  filelog offset files which the collector container's filelog receiver component writes.

The `EmptyDir` volume only exists for the lifetime of the pod.
The purpose of storing the offsets in the first place is to have them available across pod restarts, so we need to
synchronize them in some kind of permanent storage after the filelog receiver writes them to the
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
https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#providing-a-filelog-offset-volume.
