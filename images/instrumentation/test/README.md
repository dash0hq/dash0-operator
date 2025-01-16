This directory contains tests for the instrumentation image.
The difference to images/instrumentation/injector/test is that the latter tests the ld-preload injector in isolation
while the tests in this directory test the complete instrumentation image.
The tests in this directory also cover a wider variety of base images.

Usage
-----

* `./test-all.sh` to run all tests.
* `ARCHITECTURES=arm64,x86_64 ./test-all.sh` to run tests for a subset of CPU architectures.
* `RUNTIMES=node,java ./test-all.sh` to run tests for a subset of runtimes.
* `RUNTIMES=node,java BASE_IMAGES=node:22-alpine,openjdk:24-jdk-bookworm ./test-all.sh` to run tests for a subset of
  runtimes and only for a subset of base images.
* `TEST_CASES=existing,otel-resource ./test-all.sh` to only run tests cases whose names contain one of the provided
  strings. Can be combined with `ARCHITECTURES`, `RUNTIMES` etc.
* Set `PRINT_DOCKER_OUTPUT=true` to always include the output from docker build and docker run. Otherwise the output is
  only printed to stdout in case of errors.

