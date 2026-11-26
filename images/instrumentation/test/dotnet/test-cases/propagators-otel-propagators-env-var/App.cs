// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

namespace Dash0;

using System.Net;
using System.Net.Sockets;

class App
{
    // Verifies that OTEL_PROPAGATORS (set by the user or the operator) overrides the default propagators.
    private static readonly string[] ExpectedPresent = new string[] { "traceparent" };
    private static readonly string[] ExpectedAbsent = new string[] { "X-Amzn-Trace-Id" };

    static int Main(string[] args)
    {
        try
        {
            var headers = SendRequestAndCaptureHeaders();
            foreach (var header in ExpectedPresent)
            {
                if (headers[header] == null)
                {
                    throw new SystemException(
                        $"Expected the header {header} to be present, received headers: {string.Join(", ", headers.AllKeys)}");
                }
            }
            foreach (var header in ExpectedAbsent)
            {
                if (headers[header] != null)
                {
                    throw new SystemException(
                        $"Expected the header {header} to be absent, received headers: {string.Join(", ", headers.AllKeys)}");
                }
            }
        }
        catch (SystemException e)
        {
            // There is a regression in .NET 9 where a docker container does not stop when there is an unhandled
            // exception in the .NET app, so we catch the exception and terminate explicitly here.
            Console.Error.WriteLine("test failed: " + e.Message);
            return 1;
        }
        return 0;
    }

    private static System.Collections.Specialized.NameValueCollection SendRequestAndCaptureHeaders()
    {
        var portFinder = new TcpListener(IPAddress.Loopback, 0);
        portFinder.Start();
        var port = ((IPEndPoint)portFinder.LocalEndpoint).Port;
        portFinder.Stop();

        using var listener = new HttpListener();
        listener.Prefixes.Add($"http://127.0.0.1:{port}/");
        listener.Start();
        var contextTask = listener.GetContextAsync();

        using var client = new HttpClient();
        var responseTask = client.GetAsync($"http://127.0.0.1:{port}/");

        var context = contextTask.GetAwaiter().GetResult();
        var headers = context.Request.Headers;
        context.Response.StatusCode = 204;
        context.Response.Close();
        responseTask.GetAwaiter().GetResult();
        return headers;
    }
}
