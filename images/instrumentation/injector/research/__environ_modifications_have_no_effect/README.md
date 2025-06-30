Modifications Made To `__environ` By The Injector Are Ignored
=============================================================

When moving from an injector that overrides `getenv` to a version that instead exports `__environ`, we noticed something
odd: Some executables seem to "ignore" all modifications done to `__environ` by the injector LD_PRELOAD hook.
This was first seen with Node.js, but the symptom is not limited to Node.js.
The result looks like this:

```
node@24797c8d4913:/usr/src/dash0/injector/app$ LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so node -e "console.log(process.env)"
[Dash0 injector] injecting the Java OpenTelemetry agent
[Dash0 injector] injecting the Dash0 Node.js OpenTelemetry distribution
{
  LD_PRELOAD: '/dash0-init-container/injector/dash0_injector.so',
  HOSTNAME: '24797c8d4913',
  ARCH_UNDER_TEST: 'arm64',
  YARN_VERSION: '1.22.22',
  PWD: '/usr/src/dash0/injector/app',
  HOME: '/home/node',
  LS_COLORS: ....
  TERM: 'xterm',
  LIBC_UNDER_TEST: 'glibc',
  SHLVL: '1',
  PATH: '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
  NODE_VERSION: '22.15.0',
  OLDPWD: '/usr/src/dash0/injector',
  _: '/usr/local/bin/node'
}
```

That is, the injector claims to have run successfully, but none of the modifications are visible in process.env.

Or, like this (when querying for a specific environment variable):

```
node@24797c8d4913:/usr/src/dash0/injector/app$ LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so node -e "console.log(process.env.NODE_OPTIONS)"
[Dash0 injector] injecting the Java OpenTelemetry agent
[Dash0 injector] injecting the Dash0 Node.js OpenTelemetry distribution
undefined
node@24797c8d4913:/usr/src/dash0/injector/app$
```

This issue if often hidden accidentally, due to the following fact:
When running a container image (say one of the official `node` images), their [_entrypoint_](https://github.com/nodejs/docker-node/blob/main/24/bookworm/Dockerfile#L75)
is usually a [shell](https://github.com/nodejs/docker-node/blob/main/24/bookworm/docker-entrypoint.sh).
That is, when specificying a `CMD` like `node index.js`, and the LD_PRELOAD is set as an environment variable from
outside the container (which it usually is, at least in Kubernetes), then the _shell_ entrypoint will be instrumented by
the injector, and the `node` binary will inherit the  already-instrumented environment.

Another thing worth noting is that this does not behave identical for all four combinatsion of CPU architectures and
libc flavors. For Node.js it only works for `arm64/musl`, but not for `arm64/glibc`, `x86_64/glibc` or `x86_64/musl`.

```
TEST_SETS=default TEST_CASES="getenv: overrides NODE_OPTIONS if it is not present" test/scripts/test-all.sh

[...] lots of output here...

arm64/glibc:    failed
arm64/musl:     ok
x86_64/glibc:   failed
x86_64/musl:    failed
```

For another app (C app, see below), it only works for `arm64/glibc`, but not the other combinations.
That is:

```
TEST_SETS=environ-layout.tests scripts/test-all.sh

[...] lots of output here...

arm64/glibc:    ok
arm64/musl:     failed
x86_64/glibc:   failed
x86_64/musl:    failed
```

These could also be quirks of the particular images or another difference, not necessarily the CPU architecture.
It is currently unclear why the successful architecture/libc combination with these two test cases are different.
The LD_DEBUG output looks very similar in the failing cases.

Reproducing with a small C binary
---------------------------------

The effect is apparently not limited to Node.js. In fact, it can be reproduced with a small C test binary, for example
`images/instrumentation/injector/test/environ-layout/environ-layout.c`.

Here are steps to reproduce this for x86_64/glibc:

* Check out commit sha: 282e4f48633b45d3b51eccb31af3bd527416a47c or later.

```
> DOCKER_CLEANUP_ENABLED=false TEST_SETS=environ-layout.tests scripts/test-all.sh
> docker run --platform --platform linux/amd64 -it dash0-injector-test-x86_64-glibc /bin/bash
> cd compiled-apps
> LD_DEBUG=all LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so ./environlayoutapp compare-to-snapshot WITH_MODIFICATIONS &> /tmp/ld_debug_all.txt
```

The output of running this is in
`images/instrumentation/injector/research/__environ_modifications_have_no_effect/c_environ_layout/ld_debug_all_x86_64_glibc_FAIL.txt`

Here is the relevant part of the code:

```environ_layout.c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>
#include <inttypes.h>

extern char** __environ;
extern char** _environ;
extern char** environ;

...

int main(int argc, char** argv, char** envp) {
  // print __environ, _environ, environ and envp...
}
```

```Makefile
# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

CFLAGS ?= -Wall -Werror -Wextra -Wl,-z,undefs -O2

...

%.o: %.c $(DEPS)
	$(CC) $(CFLAGS) $< -o $@
	@chmod 755 $@
```

When running this with LD_PRELOAD with the Dash0 injector with arm64/glibc, the injector modifictions are visible in
the app under test. For any other combination of architecture and libc flavor, the modifications are not visible.

This is an excerpt of the LD_DEBUG=all output for (failing) x86_64/glibc case. The full output is in
`images/instrumentation/injector/research/__environ_modifications_have_no_effect/c_environ_layout/ld_debug_all_x86_64_glibc_FAIL.txt`

```
...
19:	symbol=__environ;  lookup in file=./environlayoutapp [0]
19:	binding file /lib/x86_64-linux-gnu/libc.so.6 [0] to ./environlayoutapp [0]: normal symbol `__environ' [GLIBC_2.2.5]
...
19:	symbol=__environ;  lookup in file=/dash0-init-container/injector/dash0_injector.so [0]
19:	binding file ./environlayoutapp [0] to /dash0-init-container/injector/dash0_injector.so [0]: normal symbol `__environ' [GLIBC_2.2.5]
```

That is, when the app under test requests the `__environ` symbol, it is bound to the libc version of `__environ`.
Then later, the injector requests the same symbol, and it is bound to app under test?

### Comparison With Successful Case (arm64/glibc)

For the **successful** arm64/glibc case, the relevant LD_DEBUG output looks like this. The full output is in
`images/instrumentation/injector/research/__environ_modifications_have_no_effect/c_environ_layout/ld_debug_all_arm64_glibc_SUCCESS.txt`

```
9:	symbol=__environ;  lookup in file=./environlayoutapp [0]
9:	symbol=__environ;  lookup in file=/dash0-init-container/injector/dash0_injector.so [0]
9:	binding file /lib/aarch64-linux-gnu/libc.so.6 [0] to /dash0-init-container/injector/dash0_injector.so [0]: normal symbol `__environ' [GLIBC_2.17]
...
9:	symbol=__environ;  lookup in file=./environlayoutapp [0]
9:	symbol=__environ;  lookup in file=/dash0-init-container/injector/dash0_injector.so [0]
```

A noticeable difference is that in the successfuly case, the first __environ happens almost simultaneously with a lookup
from the injector. It is still bound to libc though. Then libc is bound to the **injector**, where in the failing case
libc is bound to the app under test.

### Aside: __environ vs envp

An interesting observation is that in the successful case, also `envp` has the modifications. The mechanism for that
is also not entirely clear. My understanding is that when an executable is started, the kernel calls the system
call `execve`, which puts the current environment onto the stack. The libc (glibc, musl) would likely read that stack
frame and then put that into `__environ`, which is exported. The variable `__environ` is a userland convention, the
kernel does not know or care about it. The `envp` pointer is then passed to the `main` function by the libc's startup
code (`__libc_start_main()` or similar).
Why modifications to the `__environ` symbol are visible in `envp` in the successful case is not clear.

Reproducing With Node.js
------------------------

For Node.js, a _different_ combination of architecture and libc flavor is successful, namely arm64/musl.

### ARM64/glibc

Here are steps to reproduce this for arm64/glibc:

* Check out commit sha: cdf607aed296cd27badf596d4b960e32982f89ae (this is an earlier version without getenv override).
* `DOCKER_CLEANUP_ENABLED=false ARCHITECTURES=arm64 LIBC_FLAVORS=glibc TEST_SETS=default TEST_CASES="getenv: overrides NODE_OPTIONS if it is not present" test/scripts/test-all.sh` produces a suitable container image to conduct the test.
* Enter the container by running `docker run -it dash0-injector-test-arm64-glibc /bin/bash`.
* Run `LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so node -e "console.log(process.env)"`
* Alternatively, run the test app, that is:
    * `cd app`
    * `LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so node index.js node_options`

The output of the `LD_PRELOAD=/dash0-init-container/injector/dash0_injector.so node index.js node_options` is in
`./test-output.txt`.
The output of running the same with `LD_DEBUG=all` is in `nodejs/ld_debug_all_arm64_glibc.txt`.

The `__environ` symbol is mentioned twice, and it is bound differently.

First occurence, Line 605-606:
```
62:	symbol=__environ;  lookup in file=node [0]
62:	binding file /lib/aarch64-linux-gnu/libc.so.6 [0] to node [0]: normal symbol `__environ' [GLIBC_2.17]
```

Second occurence, line 9251-9252:
```
62:	symbol=__environ;  lookup in file=/dash0-init-container/injector/dash0_injector.so [0]
62:	binding file node [0] to /dash0-init-container/injector/dash0_injector.so [0]: normal symbol `__environ' [GLIBC_2.17]
```

```
ldd /usr/local/bin/node
	linux-vdso.so.1 (0x0000ffff9e169000)
	libdl.so.2 => /lib/aarch64-linux-gnu/libdl.so.2 (0x0000ffff9e100000)
	libstdc++.so.6 => /lib/aarch64-linux-gnu/libstdc++.so.6 (0x0000ffff9de00000)
	libm.so.6 => /lib/aarch64-linux-gnu/libm.so.6 (0x0000ffff9e060000)
	libgcc_s.so.1 => /lib/aarch64-linux-gnu/libgcc_s.so.1 (0x0000ffff9e020000)
	libpthread.so.0 => /lib/aarch64-linux-gnu/libpthread.so.0 (0x0000ffff9ddd0000)
	libc.so.6 => /lib/aarch64-linux-gnu/libc.so.6 (0x0000ffff9dc20000)
	/lib/ld-linux-aarch64.so.1 (0x0000ffff9e12c000)
```

### ARM64/musl

It would obviouly be quite interesting to get the LD_DEBUG output for the case that works (arm64/musl), but
unfortunately Alpine is a stubborn arse and refuses to provide any debug output for `LD_DEBUG=all`.

```
ldd /usr/local/bin/node
	/lib/ld-musl-aarch64.so.1 (0xffffac98c000)
	libstdc++.so.6 => /usr/lib/libstdc++.so.6 (0xffffa6000000)
	libc.musl-aarch64.so.1 => /lib/ld-musl-aarch64.so.1 (0xffffac98c000)
	libgcc_s.so.1 => /usr/lib/libgcc_s.so.1 (0xffffac95b000)
```
