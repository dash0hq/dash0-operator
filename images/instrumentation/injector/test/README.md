This directory contains isolated tests for the injector code.
The difference to images/instrumentation/test is that the latter tests the whole instrumentation image.
Also, the tests in this folder do not use multi-platform images, an injector binary is build (in a container) per CPU
architecture, and then used for testing.
