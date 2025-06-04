// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) {
        verifyEnvVar("OTEL_RESOURCE_ATTRIBUTES", null);
    }
}
