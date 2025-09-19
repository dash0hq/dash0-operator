Injector Integration Tests
==========================

This directory contains isolated integration tests for the injector code.
The difference to images/instrumentation/test is that the latter tests the whole instrumentation image.
Also, the tests in this folder do not use multi-platform images; instead, an injector binary is build (in a container)
per CPU architecture, and then used for testing.

Another subtle yet very important difference is that for the tests in this folder, the injector is set via LD_PRELOAD
for the executable under test directly.
In contrast, for the instrumentation image tests in images/instrumentation/test, it is set for the container as a whole,
and that usually means that the entrypoint (which might be a shell) is already instrumented by the injector, and the
actual executable under test inherits the already-instrumented environment.

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
* `TEST_CASES="getenv: overrides NODE_OPTIONS if it is not present" scripts/test-all.sh` to only run tests cases whose
  names _exactly match_ one of the provided strings.
  The test cases are listed in the different test sets, i.e. the `scripts/*.tests` files.
  Can be combined with `ARCHITECTURES`, `LIBC_FLAVORS` etc., cannot be combined with `TEST_CASES_CONTAINING`.
* `TEST_CASES_CONTAINING=OTEL_RESOURCE_ATTRIBUTES,OTEL_RESOURCE_ATTRIBUTES scripts/test-all.sh` to only run tests cases
  whose names _contain_ one of the provided strings as a substring.
  The test cases are listed in the different `scripts/*.tests` files.
  Can be combined with `ARCHITECTURES`, `LIBC_FLAVORS` etc., cannot be combined with `TEST_CASES`.
* `INSTRUMENTATION_IMAGE=... scripts/test-all.sh` use an existing local or remote instrumentation image.
* Set `VERBOSE=true` to always include the output from running the test case. Otherwise, the output is only
  printed to stdout when a test case fails.
* `MISSING_ENVIRON_SYMBOL_TESTS=true` also run tests with a binary that does not provide an `__environ` symbol.
  These are currently off by default.
