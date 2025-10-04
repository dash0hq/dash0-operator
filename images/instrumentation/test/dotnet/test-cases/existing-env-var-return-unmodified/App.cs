// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

class App
{
    static int Main(string[] args)
    {
        try
        {
            VerifyEnvVar("AN_ENVIRONMENT_VARIABLE", "value");
        }
        catch (SystemException e)
        {
            // There is a regression in .NET 9 where a docker container does not stop when there is an unhandled
            // exception in the .NET app, so we catch the exception and terminate explicitly here.
            // See https://github.com/dotnet/runtime/issues/118049 and https://github.com/dotnet/runtime/issues/112580.
            Console.Error.WriteLine("test failed: " + e.Message);
            return 1;
        }
        return 0;
    }

    private static void VerifyEnvVar(String envVarName, String expected)
    {
        string actual = Environment.GetEnvironmentVariable(envVarName);
        if (expected == null)
        {
            if (actual != null) {
                throw new SystemException(
                        String.Format(
                                "Unexpected value for the \"{0}\" --\n" +
                                        "- expected: null,\n" +
                                        "- was:      \"{1}\"",
                                envVarName,
                                actual
                        ));
            }
        }
        if (actual != expected) {
            throw new SystemException(
                    String.Format(
                            "Unexpected value for the \"{0}\" --\n" +
                                    "expected: \"{1}\",\n" +
                                    "was:      \"{2}\"",
                            envVarName,
                            expected,
                            actual
                    ));
        }
    }
}
