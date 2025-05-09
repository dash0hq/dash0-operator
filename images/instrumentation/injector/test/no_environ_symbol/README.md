This Go application reproduces an issue where the injector `LD_PRELOAD` hook would crash executables that do not have
the `__environ` symbol.
This can happen for example for Go applications built with `-buildmode=pie`.
