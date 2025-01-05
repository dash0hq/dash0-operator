# JVM instrumentation

We currently do not have a Dash0 distro for Java.
Rather, we use the upstream [OpenTelemetry Java agent](https://github.com/open-telemetry/opentelemetry-java-instrumentation).
The version of the OTel Java agent to be used is specified in the [local `pom.xml`](./pom.xml) file.
Maven can download the correct version of the OTel Java agent by running:

```shell
./mvnw dependency:copy-dependencies
```