// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

using System.Diagnostics;
using System.Runtime.Loader;

class App
{
    private const string DistributionPathPrefix = "/__otel_auto_instrumentation/agents/dotnet/";

    // Must match OTEL_DOTNET_AUTO_TRACES_ADDITIONAL_SOURCES in the .env file of this test case.
    private const string ActivitySourceName = "Dash0.Test.VerifyDistroIsLoaded";

    static int Main(string[] args)
    {
        try
        {
            VerifyNativeProfilerIsAttached();
            VerifyManagedInstrumentationIsLoaded();
            VerifySdkIsActive();
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

    private static void VerifyNativeProfilerIsAttached()
    {
        bool profilerAttached = File.ReadLines("/proc/self/maps").Any(line =>
            line.Contains(DistributionPathPrefix) &&
            line.EndsWith("/OpenTelemetry.AutoInstrumentation.Native.so"));
        if (!profilerAttached)
        {
            throw new SystemException(
                "It looks like the native profiler of the Dash0 .NET OpenTelemetry distribution has not been " +
                "attached, OpenTelemetry.AutoInstrumentation.Native.so is not mapped into the process.");
        }
    }

    private static void VerifyManagedInstrumentationIsLoaded()
    {
        // The auto-instrumentation loads its assemblies into custom assembly load contexts, hence all load contexts
        // need to be searched, not only the default one.
        var assembly = AssemblyLoadContext.All
            .SelectMany(context => context.Assemblies)
            .FirstOrDefault(a => a.GetName().Name == "OpenTelemetry.AutoInstrumentation");
        if (assembly == null)
        {
            throw new SystemException(
                "It looks like the Dash0 .NET OpenTelemetry distribution has not been loaded, the assembly " +
                "OpenTelemetry.AutoInstrumentation is not loaded.");
        }
        if (!assembly.Location.StartsWith(DistributionPathPrefix))
        {
            throw new SystemException(
                String.Format(
                    "The assembly OpenTelemetry.AutoInstrumentation has not been loaded from the Dash0 .NET " +
                            "OpenTelemetry distribution --\n" +
                            "expected location prefix: \"{0}\",\n" +
                            "was:                      \"{1}\"",
                    DistributionPathPrefix,
                    assembly.Location
                ));
        }
    }

    private static void VerifySdkIsActive()
    {
        using var source = new ActivitySource(ActivitySourceName);
        using var activity = source.StartActivity("verify-distro-is-loaded");
        if (activity == null)
        {
            throw new SystemException(
                "It looks like the OpenTelemetry SDK is not active, no tracer provider is listening to the " +
                "activity source \"" + ActivitySourceName + "\".");
        }
    }
}
