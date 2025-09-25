// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) {
        // verify that we do not interfere with other existing -D properties that are provided directly on the command
        // line (not via JAVA_TOOL_OPTIONS)
        verifyProperty("another.system.property", "value");

        // verify that we inject OTEL_RESOURCE_ATTRIBUTES correctly
        verifyEnvVar(
                "OTEL_RESOURCE_ATTRIBUTES",
                "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name"
        );
    }
}
