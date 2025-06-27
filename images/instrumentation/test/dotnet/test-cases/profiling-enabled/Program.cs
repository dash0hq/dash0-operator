// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

using System.Text.RegularExpressions;

class Program
{
    static int Main(string[] args)
    {
        verifyEnvVar("DASH0_EXPERIMENTAL_DOTNET_INJECTION", "true");
        verifyEnvVar("CORECLR_ENABLE_PROFILING", "1");
        verifyEnvVar("CORECLR_PROFILER", "{918728DD-259F-4A6A-AC2B-B85E1B658318}");
        matchesRegex("CORECLR_PROFILER_PATH", @"^/__dash0__/instrumentation/dotnet/(?:musl|glibc)/linux-(?:musl-)?(?:arm64|x64)/OpenTelemetry.AutoInstrumentation.Native.so$");
        matchesRegex("DOTNET_ADDITIONAL_DEPS", @"^/__dash0__/instrumentation/dotnet/(?:musl|glibc)/AdditionalDeps$");
        matchesRegex("DOTNET_SHARED_STORE", @"^/__dash0__/instrumentation/dotnet/(?:musl|glibc)/store$");
        matchesRegex("DOTNET_STARTUP_HOOKS", @"^/__dash0__/instrumentation/dotnet/(?:musl|glibc)/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll$");
        matchesRegex("OTEL_DOTNET_AUTO_HOME", @"^/__dash0__/instrumentation/dotnet/(?:musl|glibc)$");
        return 0;
    }

    private static void verifyEnvVar(string envVarName, string expectedValue)
    {
        string? actualValue = Environment.GetEnvironmentVariable(envVarName);
        if (expectedValue == null)
        {
        	if (actualValue != null)
        	{
				Console.Error.WriteLine($"Unexpected value for {envVarName}: expected: null, was: \"{actualValue}\"");
				System.Environment.Exit(1);
			}
			else
			{
				Console.Error.WriteLine($"Expected value for {envVarName}: null.");
				return;
			}
        }
        if (actualValue == null)
        {
            Console.Error.WriteLine($"Unexpected value for {envVarName}: expected: \"{expectedValue}\", was: null");
            System.Environment.Exit(1);
        }
        else if (actualValue != expectedValue)
        {
            Console.Error.WriteLine($"Unexpected value for {envVarName}: expected: \"{expectedValue}\", was: \"{actualValue}\"");
            System.Environment.Exit(1);
        }
        else
        {
            Console.Error.WriteLine($"Expected value for {envVarName}: \"{actualValue}\"");
        }
    }

    private static void matchesRegex(string envVarName, string pattern)
    {
        Regex regex = new Regex(pattern);
        string? actualValue = Environment.GetEnvironmentVariable(envVarName);
        if (actualValue == null)
        {
            Console.Error.WriteLine($"Unexpected value for {envVarName}: expected: a string matching \"{regex}\", was: null");
            System.Environment.Exit(1);
        }
        else if (!regex.Match(actualValue).Success)
        {
            Console.Error.WriteLine($"Unexpected value for {envVarName}: expected: a string matching \"{regex}\", was: \"{actualValue}\"");
             System.Environment.Exit(1);
        }
        else
        {
             Console.Error.WriteLine($"Matching value for {envVarName}: \"{actualValue}\"");
        }
    }
}
