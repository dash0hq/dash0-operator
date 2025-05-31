// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

class Program
{
    static int Main(string[] args)
    {
        string? dotNetInjectionEnabled = Environment.GetEnvironmentVariable("DASH0_EXPERIMENTAL_DOTNET_INJECTION");
        if (dotNetInjectionEnabled != null) {
            Console.Error.WriteLine("DASH0_EXPERIMENTAL_DOTNET_INJECTION: " + dotNetInjectionEnabled);
        } else {
            Console.Error.WriteLine("DASH0_EXPERIMENTAL_DOTNET_INJECTION is not set.");
        }

        // This will not work when the injector only overrides getenv, see the comment in
        // images/instrumentation/injector/src/dotnet.zig - overriding getenv only works for CLR bootstrap code, not
        // for environment variable lookups from within a .NET application.
        //
        // So, instead of having an actual test here that verifies that the environment variables for profiling have
        // been injected, the best we can currently do is to check that the CLR does not crash when we the injector
        // instruments it.
        // The e2e test suite has tests that verify that a .NET workload produces spans, so at least that test suite
        // verifies that the instrumentation works as expected.
        //
        // string? profilingEnabled = Environment.GetEnvironmentVariable("DOTNET_STARTUP_HOOKS");
        // if (profilingEnabled == null) {
        //     Console.Error.WriteLine("profiling is _not_ enabled - CORECLR_ENABLE_PROFILING is not set");
        //     return 1;
        // } else if (profilingEnabled == "1") {
        //     Console.Error.WriteLine("profiling is enabled - CORECLR_ENABLE_PROFILING: \"" + profilingEnabled + "\"");
        //     return 0;
        // } else {
        //     Console.Error.WriteLine("profiling is _not_ enabled - CORECLR_ENABLE_PROFILING: \"" + profilingEnabled + "\"");
        //     return 1;
        // }

        return 0;
    }
}
