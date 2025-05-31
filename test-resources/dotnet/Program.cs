// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

var builder = WebApplication.CreateBuilder(args);

var port = Environment.GetEnvironmentVariable("PORT") ?? "1407";
builder.WebHost.UseUrls($"http://0.0.0.0:{port}");

var app = builder.Build();

app.MapGet("/ready", () => Results.NoContent());

app.MapGet("/dash0-k8s-operator-test", (string? id) =>
{
    if (!string.IsNullOrEmpty(id))
    {
        Console.WriteLine($"processing request {id}");
    }
    else
    {
        Console.WriteLine("processing request");
    }

    return new { message = "We make Observability easy for every developer." };
});

app.Run();
