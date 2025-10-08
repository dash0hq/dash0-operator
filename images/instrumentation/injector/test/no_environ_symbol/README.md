This Go application serves as a regression test for an issue where an earlier invocation of the injector would crash
executables that do not have the `__environ` symbol, but are still subject to dynamic linking at startup.
This was an issue because the injector previously declared a dependency on `__environ`, with the assumption that
_any_ dynamically linked executable would also bind to a libc that exports that symbol.
This would happen for example for Go applications built with `-buildmode=pie`.
Applications with the usual `-ldflags '-extldflags "-static"'` would not be affected.
Nowadays, this is no longer an issue, since the current injector does not depend on any external symbols being present
at link time.
(Instead, it looks up the `dlsym` symbol dynamically at startup, and simply stands down if that symbol is not found.)
