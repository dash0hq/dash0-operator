This Go application reproduces an issue where the injector `LD_PRELOAD` hook would crash executables that do not have
the `__environ` symbol.
This can happen for example for Go applications built with `-buildmode=pie`.
The fix is to have a fallback in place in the injector, to prevent crashing with
`symbol lookup error: ./dash0_injector.so: undefined symbol: __environ`.
The app in this directory is a regression test for this problem.
