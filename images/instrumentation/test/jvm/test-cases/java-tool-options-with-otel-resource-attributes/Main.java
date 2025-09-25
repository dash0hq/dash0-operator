// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import static com.dash0.injector.testutils.TestUtils.*;

public class Main {
    public static void main(String[] args) {
        verifyProperty(
                "otel.resource.attributes",
                "key1=value1,key2=value2"
        );

        verifyEnvVar(
                "OTEL_RESOURCE_ATTRIBUTES",
                "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name"
        );
    }
}
