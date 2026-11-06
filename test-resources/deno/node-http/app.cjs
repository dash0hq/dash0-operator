// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// A Deno application that uses Deno's Node.js compatibility layer (CommonJS, node:http) instead of Deno's native APIs.

const http = require('node:http');
const process = require('node:process');

const port = parseInt(process.env.PORT || '1707');

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  if (req.method === 'GET' && url.pathname === '/ready') {
    res.writeHead(204);
    res.end();
    return;
  }
  if (req.method === 'GET' && url.pathname === '/dash0-k8s-operator-test') {
    const reqId = url.searchParams.get('id');
    if (reqId) {
      console.log(`processing request ${reqId}`);
    } else {
      console.log(`processing request`);
    }
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ message: 'We make Observability easy for every developer.' }));
    return;
  }
  res.writeHead(404);
  res.end();
});

server.listen(port, () => {
  console.log(`listening on port ${port}`);
});

['SIGINT', 'SIGTERM'].forEach(signalName => {
  process.on(signalName, () => {
    console.log(`received ${signalName}, stopping server`);
    server.close(() => {
      console.log('server stopped, bye');
      process.exit(0);
    });
  });
});
