Instumentation Image Integration Tests
======================================

This directory contains integration tests for the instrumentation image.
The difference to images/instrumentation/injector/test is that the latter tests the ld-preload injector in isolation
while the tests in this directory test the complete instrumentation image.
The tests in this directory also cover a wider variety of runtimes and base images.

Another subtle yet very important difference is that for the injector integration tests in
images/instrumentation/injector/test, the injector is set via LD_PRELOAD for the executable under test directly.
In contrast, for the instrumentation image tests in this folder, LD_PRELOAD is set for the container as a whole,
and that usually means that the entrypoint (which might be a shell) is already instrumented by the injector, and the
actual executable under test inherits the already-instrumented environment.

The general flow for the tests is:
* Build the instrumentation image locally, as a multi-platform image.
* Build all required test images (as single-platform images).
    * A test image is determined by a combination of CPU architecture, runtime, and base image.
    * For example, the test image `instrumentation-image-test-x86_64-jvm-openjdk-21-jdk-bookworm` runs tests for the CPU
      architecture `x86_64`, the JVM runtime and is based on the `openjdk-21-jdk-bookworm` base image, and it is built
      from `images/instrumentation/test/jvm/Dockerfile`.
      Another example: The test image `instrumentation-image-test-arm64-node-node-22-alpine` runs tests for the arm64
      architecture for Node.js based on the `node:22-alpine` base image, being built from
      `images/instrumentation/test/node/Dockerfile`.
    * All image build tasks are run in parallel, the number of concurrent builds is limited by the number of available
      CPUs, or configured via the `CONCURRENCY` environment variable.
    * The tested runtimes are represented as subfolders in the `test` directory, that is, `c`, `dotnet`, `jvm`, `node`
      etc.
      Each runtime folder contains a `Dockerfile` that builds the test image for that runtime.
    * The available base images are defined per runtime, in a file named `base-images` (in the respective runtime
      folder).
* For each CPU architecture, runtime and base image, iterate over all test cases for that runtime.
    * The test cases are each defined as subfolders in `$runtime/test-cases`, per runtime.
      Each test case is an application, written in the language for the respective runtime.
    * Test cases are expected to either end with exit code 0 (which means the test case has passed) or a non-zero exit
      code (which means the test case has failed).

Usage
-----

* `npm run test` to run all tests.
* `ARCHITECTURES=arm64,x86_64 npm run test` to run tests for a subset of CPU architectures.
* `RUNTIMES=c,dotnet,jvm,node npm run test` to run tests for a subset of runtimes.
* `RUNTIMES=node,jvm BASE_IMAGES=openjdk:24-jdk-bookworm,openjdk:21-jdk-bookworm npm run test` to run tests for a subset
  of runtimes and only for a subset of base images. Note that base images names are usually different per runtime, see
  the `base-images` file in the respective runtime directory.
* `TEST_CASES=non-existing-env-var-return-null,otel-resource-attributes-already-set, npm run test` to only run tests
  cases whose names match one of the provided strings. Can be combined with `ARCHITECTURES`, `RUNTIMES` etc.
* Set `VERBOSE=true` to always include the output from docker build, docker run and test commands. Otherwise, the output
  is only printed to stdout in case of errors.
* Set `SUPPRESS_SKIPPED=true` to reduce the output for skipped tests (useful when focussing on a small set of test cases with `TEST_CASES=...`).
* Set `DOCKER_CLEANUP_ENABLED=false` to disable the automatic docker rmi at the end of the test suites that deletes all
  images that were built during the tests.

Experimental: Run Instrumentation Image Tests Within the Dev Container
----------------------------------------------------------------------

For faster feeback cycles, you can also run tests within the injector dev container.
The support for this approach is currently experimental.
Not all test cases will actually work in the dev container, in particular the tests that require the instrumentation
assets (Dash0 Node.js OTel distribution, OTel Java agent, etc.) to be present in the image will not work.
Also, the behavior might be different, since for some test cases it matters whether the LD_PRELOAD is already set for
the container entrypoint (which is the case when running the tests in the default mode with `npm run test`) or if it
is only set directly for the executable under test (which is the case when running the tests in the dev container).

To run the tests in the dev container:
* start the dev container with the `images/instrumentation/start-injector-dev-container.sh` script,
* in the running container, cd to `/home/dash0/instrumentation/test`, then
* run `npm run test-within-container` to start tests.

The same environment variables as described above (`RUNTIMES`, `TEST_CASES`) can be used to control which tests to run.
Some environment variables like `ARCHITECTURES` and `BASE_IMAGES` are not supported with `npm run test-within-container`
(since the dev container is built and started with exactly one CPU architecture and image, so these become meaningless).
You can however use similar variables when building/starting the dev container itself, for example:
`ARCHITECTURE=x86_64 BASE_IMAGE=alpine:3.21.3 ./start-injector-dev-container.sh`, which builds and starts the container
with a specific CPU architecture and base image.
