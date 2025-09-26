package com.dash0.injector.testutils;

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
}
