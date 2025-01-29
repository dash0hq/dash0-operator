# TODO for the Zig Injector

The current approach with reading `/proc/self/environ` is hosed, because it does not allow us to react to apps setting stuff in their own environment and expecting to see the changes reflected the next time they ask for an env var.
We really need to do like the C injector and be linked at runtime the `__environ__` symbol, and read it anew every time.
(Which also has the nice side-effect that we need to allocate no memory at all for env vars we do not modify!)

Also, we need to test what happens if LibC decides to *relocate* the environment, e.g., becausde the app is setting environment variables that don't fit in the current memory segment allocated and pointed at by `__environ__`.