// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace OpenTelemetry;

using System;
using System.Runtime.InteropServices;

class App
{
    static int Main(string[] args)
    {

        string baseImageRun = Environment.GetEnvironmentVariable("BASE_IMAGE_RUN");
        if (baseImageRun == null)
        {
            Console.Error.WriteLine("BASE_IMAGE_RUN is not set");
            return 1;
        }
        string libcFlavor;
        if (baseImageRun.Contains("alpine"))
        {
            libcFlavor = "musl";
        }
        else
        {
            libcFlavor = "glibc";
        }

        string cpuArch = RuntimeInformation.ProcessArchitecture.ToString();
        if (cpuArch == "X64")
        {
            if (libcFlavor == "musl")
            {
                cpuArch = "linux-musl-x64";
            }
            else
            {
                cpuArch = "linux-x64";
            }
        }
        else if (cpuArch == "Arm64")
        {
            if (libcFlavor == "musl")
            {
                cpuArch = "linux-musl-arm64";
            }
            else
            {
                cpuArch = "linux-arm64";
            }
        }
        else
        {
            Console.Error.WriteLine("Unsupported CPU architecture: " + cpuArch);
            return 1;
        }

        string prefixLibcArch = String.Format("/__otel_auto_instrumentation/agents/dotnet/{0}/{1}", libcFlavor, cpuArch);
        string prefixLibc = String.Format("/__otel_auto_instrumentation/agents/dotnet/{0}", libcFlavor);

        try
        {
            VerifyEnvVar("CORECLR_ENABLE_PROFILING", "1");
            VerifyEnvVar("CORECLR_PROFILER", "{918728DD-259F-4A6A-AC2B-B85E1B658318}");
            VerifyEnvVar("CORECLR_PROFILER_PATH", prefixLibcArch + "/OpenTelemetry.AutoInstrumentation.Native.so");
            VerifyEnvVar("DOTNET_ADDITIONAL_DEPS", prefixLibc + "/AdditionalDeps");
            VerifyEnvVar("DOTNET_SHARED_STORE", prefixLibc + "/store");
            VerifyEnvVar("DOTNET_STARTUP_HOOKS", prefixLibc + "/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll");
            VerifyEnvVar("OTEL_DOTNET_AUTO_HOME", prefixLibc);
            return 0;
        }
        catch (SystemException e)
        {
            // There is a regression in .NET 9 where a docker container does not stop when there is an unhandled
            // exception in the .NET app, so we catch the exception and terminate explicitly here.
            // See https://github.com/dotnet/runtime/issues/118049 and https://github.com/dotnet/runtime/issues/112580.
            Console.Error.WriteLine("test failed: " + e.Message);
            return 1;
        }
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
