# Invoke with:
# /use-local-collector-component kubeletstatsreceiver
# /use-local-collector-component --cleanup kubeletstatsreceiver
---
name: use-local-collector-component
description: Use this skill to prepare the dash0-operator repository for using an OpenTelemetry collector component that is built from local sources. Pass --cleanup to revert the changes.
disable-model-invocation: true
---

If $ARGUMENTS starts with `--cleanup`, extract the component name (the part after `--cleanup `) and perform the cleanup steps below. Otherwise perform the setup steps below.

## Setup

* Find out which repository contains the component $ARGUMENTS. It is either from https://github.com/open-telemetry/opentelemetry-collector-contrib or https://github.com/open-telemetry/opentelemetry-collector.
  The components are found in one of the following directories in one of these two repositories:
    - `connector`
    - `exporter`
    - `extension`
    - `processor`
    - `receiver`
* Clone the repository which contains the component into images/collector.
  Clone the tag that matches the current component version in images/collector/src/builder/config.yaml, e.g. v0.147.0.
  If the directory already exists, ask if it can be deleted.
* Add a COPY instruction to the "builder" stage of images/collector/Dockerfile, after the WORKDIR instruction.
  The COPY instruction copies the sources for the component $ARGUMENTS.
  Example:
  ```
  COPY opentelemetry-collector-contrib/receiver/kubeletstatsreceiver opentelemetry-collector-contrib/receiver/kubeletstatsreceiver
  ```
* Add a "replaces" section to the end of images/collector/src/builder/config.yaml, with one directive to replace the
  published component with the locally built component.
  Example:
  ```
  replaces:
  - github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver => ../opentelemetry-collector-contrib/receiver/kubeletstatsreceiver
  ```

## Cleanup

* Find out which repository contains the component (same logic as in setup).
* Delete the cloned repository directory from images/collector (e.g. images/collector/opentelemetry-collector-contrib or images/collector/opentelemetry-collector).
  Only delete it if it exists. If it does not exist, skip this step without error.
* Remove the COPY instruction for the component from the "builder" stage of images/collector/Dockerfile.
  Only remove the specific COPY line for this component, leaving all other COPY instructions intact.
  If no such COPY instruction exists, skip this step without error.
* Remove the replaces directive for the component from images/collector/src/builder/config.yaml.
  If the `replaces` section becomes empty after removing the directive, remove the entire `replaces` section.
  If no such directive exists, skip this step without error.
