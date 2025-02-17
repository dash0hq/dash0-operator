This directory contains isolated tests for the injector code.
The difference to images/instrumentation/test is that the latter tests the whole instrumentation image.
Also, the tests in this folder do not use multi-platform images, an injector binary is build (in a container) per CPU
architecture, and then used for testing.

The test cases are listed in `run-tests-within-container.sh`.

Usage
-----

* `scripts/test-all.sh` to run all tests.
* `ARCHITECTURES=arm64,x86_64 scripts/test-all.sh` to run tests for a subset of CPU architectures.
* `LIBC_FLAVORS=glibc,musl scripts/test-all.sh` to run tests for a subset of libc flavors. Can be combined with
  `ARCHITECTURES`.
* `TEST_CASES=twice,mapped scripts/test-all.sh` to only run tests cases whose names contain one of the
  provided strings.
  The test cases are listed in `run-tests-within-container.sh`.
  Can be combined with `ARCHITECTURES` and `LIBC_FLAVORS`.
* `INSTRUMENTATION_IMAGE scripts/test-all.sh` use an existing local or remote instrumentation image.

