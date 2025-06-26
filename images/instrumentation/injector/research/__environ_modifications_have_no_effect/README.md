Node.js Ignores Modifications Made To `__environ` By The Injector
=================================================================

When moving from an injector that overrides `getenv` to a version that instead exports `__environ`, we noticed something
odd: Node.js seems to "ignore" the modifications done to `__environ` by the injector LD_PRELOAD.
The result looks like this.

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
libc flavors:

```
TEST_SETS=default TEST_CASES="getenv: overrides NODE_OPTIONS if it is not present" test/scripts/test-all.sh

[...] lots of output here...

arm64/glibc:	failed
arm64/musl:	ok
x86_64/glibc:	failed
x86_64/musl:	failed
```

That is, with an ARM64 architecture and an Alpine image, the injection works as expected.
That could also be a quirk of that particular image or another difference.

Reproducing
-----------

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
The output of running the same with `LD_DEBUG=all` is in `./ld_debug_all.txt`.

The `__environ` symbol is mentioned twice, and it is bound differently.

First occurence, Line 605-606:
```
62:	symbol=__environ;  lookup in file=node [0]
62:	binding file /lib/aarch64-linux-gnu/libc.so.6 [0] to node [0]: normal symbol `__environ' [GLIBC_2.17]
```

? Does `node` export `__environ`?

Second occurence, line 9251-9252:
```
62:	symbol=__environ;  lookup in file=/dash0-init-container/injector/dash0_injector.so [0]
62:	binding file node [0] to /dash0-init-container/injector/dash0_injector.so [0]: normal symbol `__environ' [GLIBC_2.17]
```

It would obviouly be quite interesting to get the LD_DEBUG output for the case that works (arm64/musl), but
unfortunately Alpine is a stubborn arse and refuses to provide any debug output for `LD_DEBUG=all`.
