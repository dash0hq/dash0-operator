// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// A Deno application that only uses Deno's native APIs (Deno.serve), without Deno's Node.js compatibility layer.

const port = parseInt(Deno.env.get('PORT') || '1807');

const controller = new AbortController();

const server = Deno.serve(
  {
    port,
    signal: controller.signal,
    onListen: () => {
      console.log(`listening on port ${port}`);
    },
  },
  (req: Request): Response => {
    const url = new URL(req.url);
    if (req.method === 'GET' && url.pathname === '/ready') {
      return new Response(null, { status: 204 });
    }
    if (req.method === 'GET' && url.pathname === '/dash0-k8s-operator-test') {
      const reqId = url.searchParams.get('id');
      if (reqId) {
        console.log(`processing request ${reqId}`);
      } else {
        console.log(`processing request`);
      }
      return Response.json({ message: 'We make Observability easy for every developer.' });
    }
    return new Response(null, { status: 404 });
  },
);

(['SIGINT', 'SIGTERM'] as const).forEach(signalName => {
  Deno.addSignalListener(signalName, () => {
    console.log(`received ${signalName}, stopping server`);
    controller.abort();
  });
});

await server.finished;
console.log('server stopped, bye');
