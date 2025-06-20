// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

class Program
{
    static int Main(string[] args)
    {
        verifyEnvVar("OTEL_RESOURCE_ATTRIBUTES", "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name");
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
}
