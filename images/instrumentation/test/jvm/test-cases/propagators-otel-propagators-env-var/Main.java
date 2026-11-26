// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import com.sun.net.httpserver.Headers;

import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) throws Exception {
        // verify that OTEL_PROPAGATORS (set by the user or the operator) overrides the default propagators
        Headers headers = sendRequestAndCaptureHeaders();
        verifyHeaderPresent(headers, "traceparent");
        verifyHeaderAbsent(headers, "X-Amzn-Trace-Id");
    }
}
