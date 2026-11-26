package com.dash0.injector.testutils;

import com.sun.net.httpserver.Headers;
import com.sun.net.httpserver.HttpServer;

import java.net.HttpURLConnection;
import java.net.InetSocketAddress;
import java.net.URL;
import java.util.concurrent.atomic.AtomicReference;

public class TestUtils {

    public static void verifyEnvVar(String envVarName, String expected) {
        compare(envVarName, "environment variable", expected, System.getenv(envVarName));
    }

    public static void verifyProperty(String propertyName, String expected) {
        compare(propertyName, "property", expected, System.getProperty(propertyName));
    }

    public static void compare(String propertyName, String label, String expected, String actual) {
        if (expected == null) {
            if (actual != null) {
                throw new RuntimeException(
                        String.format(
                                "Unexpected value for the %s \"%s\" --\n" +
                                        "- expected: null,\n" +
                                        "- was:      \"%s\"",
                                label,
                                propertyName,
                                actual
                        ));
            }
            return;
        }
        if (!expected.equals(actual)) {
            throw new RuntimeException(
                    String.format(
                            "Unexpected value for the %s \"%s\" --\n" +
                                    "expected: \"%s\",\n" +
                                    "was:      \"%s\"",
                            label,
                            propertyName,
                            expected,
                            actual
                    ));
        }
    }

    /**
     * Sends an HTTP request via HttpURLConnection to a local HTTP server and returns the request headers the server has
     * received. Requires the OpenTelemetry Java agent's http-url-connection instrumentation to be enabled for trace
     * context headers to be present.
     */
    public static Headers sendRequestAndCaptureHeaders() throws Exception {
        AtomicReference<Headers> received = new AtomicReference<>();
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", exchange -> {
            received.set(exchange.getRequestHeaders());
            exchange.sendResponseHeaders(204, -1);
            exchange.close();
        });
        server.start();
        try {
            URL url = new URL("http://127.0.0.1:" + server.getAddress().getPort() + "/");
            HttpURLConnection connection = (HttpURLConnection) url.openConnection();
            int status = connection.getResponseCode();
            connection.disconnect();
            if (status != 204) {
                throw new RuntimeException("Unexpected HTTP status code: " + status);
            }
        } finally {
            server.stop(0);
        }
        return received.get();
    }

    public static void verifyHeaderPresent(Headers headers, String name) {
        if (headers.getFirst(name) == null) {
            throw new RuntimeException(
                    String.format("Expected the header \"%s\" to be present, received headers: %s", name, headers.keySet()));
        }
    }

    public static void verifyHeaderAbsent(Headers headers, String name) {
        if (headers.getFirst(name) != null) {
            throw new RuntimeException(
                    String.format("Expected the header \"%s\" to be absent, received headers: %s", name, headers.keySet()));
        }
    }
}
