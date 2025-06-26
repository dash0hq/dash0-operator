Injector Integration Tests
==========================

This directory contains isolated integration tests for the injector code.
The difference to images/instrumentation/test is that the latter tests the whole instrumentation image.
Also, the tests in this folder do not use multi-platform images; instead, an injector binary is build (in a container)
per CPU architecture, and then used for testing.
Note that the Zig source code in images/instrumentation/injector/src also contains Zig unit tests.

The available test cases for the injector integration tests are listed in the files
`images/instrumentation/injector/test/scripts/*.tests`.

Usage
-----

* `scripts/test-all.sh` to run all tests.
* `ARCHITECTURES=arm64,x86_64 scripts/test-all.sh` to run tests for a subset of CPU architectures.
  Can be combined with `LIBC_FLAVORS` and other flags.
* `LIBC_FLAVORS=glibc,musl scripts/test-all.sh` to run tests for a subset of libc flavors.
  Can be combined with `ARCHITECTURES` and other flags.
* `TEST_SETS=default,sdk-cannot-be-accessed` to only run a subset of test sets  The test set names are the different
  `scripts/*.tests` files. Can be combined with `ARCHITECTURES`, `LIBC_FLAVORS` etc.
* `TEST_CASES=twice,mapped scripts/test-all.sh` to only run tests cases whose names exactly match one of the
  provided strings.
  The test cases are listed in the different test sets, i.e. the `scripts/*.tests` files.
  Can be combined with `ARCHITECTURES`, `LIBC_FLAVORS` etc.
* `INSTRUMENTATION_IMAGE=... scripts/test-all.sh` use an existing local or remote instrumentation image.
* Set `VERBOSE=true` to always include the output from running the test case. Otherwise, the output is only
  printed to stdout when a test case fails.
* `STATICALLY_BUILT_TESTS=true` also run tests with a binary that has been statically built and linked.
  These are currently off by default.
