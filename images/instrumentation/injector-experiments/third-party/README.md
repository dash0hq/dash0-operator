Use this directory to git clone third-party sources of runtimes, like the JDK (https://github.com/openjdk/jdk) or
Node.js (https://github.com/nodejs/node) from source, which can be helpful to investigate segfaults or other nasties.
The rebuild-and-run.sh scripts in ../zig-to-jvm and ../zig-to-node can be told to use the locally built runtime instead
of the preinstalled one with minor modifications.