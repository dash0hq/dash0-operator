Instumentation Image Integration Tests
======================================

This directory contains integration tests for the instrumentation image.
The difference to images/instrumentation/injector/test is that the latter tests the ld-preload injector in isolation
while the tests in this directory test the complete instrumentation image.
The tests in this directory also cover a wider variety of runtimes and base images.

The general flow for the tests is:
* Build the instrumentation image locally, as a multi-platform image.
* Build all required test images (as single-platform images).
    * A test image is determined by a combination of CPU architecture, runtime, and base image.
    * For example, the test image `instrumentation-image-test-x86_64-jvm-openjdk-21-jdk-bookworm` runs test for the CPU
  architecture `x86_64`, the JVM runtime and is based on the `openjdk-21-jdk-bookworm` base image.
      The test image `instrumentation-image-test-arm64-node-node-22-alpine` runs test for the arm64 architecture for
      Node.js based on the `node:22-alpine` base image.
    * All image build tasks are run in parallel, the number of concurrent builds is limited by the number of available
      CPUs, or configured via the `CONCURRENCY` environment variable.
    * The tested runtimes are represented as subfolders in the `test` directory, that is, `c`, `dotnet`, `jvm`, `node`
      etc.
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
* `RUNTIMES=node,jvm npm run test` to run tests for a subset of runtimes.
* `RUNTIMES=node,jvm BASE_IMAGES=openjdk:24-jdk-bookworm,openjdk:21-jdk-bookworm npm run test` to run tests for a subset
  of runtimes and only for a subset of base images. Note that base images names are usually different per runtime, see
  the `base-images` file in the respective runtime directory.
* `TEST_CASES=existing,otel-resource npm run test` to only run tests cases whose names contain one of the provided
  strings. Can be combined with `ARCHITECTURES`, `RUNTIMES` etc.
* Set `VERBOSE=true` to always include the output from docker build, docker run and test commands. Otherwise, the output
  is only printed to stdout in case of errors.
* Set `DOCKER_CLEANUP_ENABLED=false` to disable the automatic docker rmi at the end of the test suites that deletes all
  images that were built during the tests.

Start Tests Within the Dev Container
------------------------------------

TODO...

