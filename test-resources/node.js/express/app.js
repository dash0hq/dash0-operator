// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const express = require('express');

const port = parseInt(process.env.PORT || '1207');
const app = express();

app.get('/ready', (req, res) => {
    res.sendStatus(204);
});

app.get('/dash0-k8s-operator-test', (req, res) => {
  console.log(`processing request ${req.query['id']}`);
  res.json({ message: 'We make Observability easy for every developer.' });
});

const server = app.listen(port, () => {
  console.log(`listening on port ${port}`);
});

if (process.env.TRIGGER_SELF_AND_EXIT) {
  (async function () {
    const testId = process.env.TEST_ID;
    if (!testId) {
      console.error('TEST_ID environment variable is not set, exiting');
      process.exit(1);
    }

    for (let i = 0; i < 120; i++) {
      await fetch(`http://localhost:${port}/dash0-k8s-operator-test?id=${testId}`);
      await delay(500);
    }

    process.exit(0);
  })();
}

function delay(ms) {
  return new Promise(resolve => {
    setTimeout(resolve, ms);
  });
}

// When running in Docker/K8s (as PID 1), just sending SIGINT/SIGTERM does not stop the application for some reason (
// although both signals work as expected when running the application directly on a host). We fix this with an explicit
// signal handler. Without this, deleting the pod in K8s takes 30-40 seconds.
['SIGINT', 'SIGTERM'].forEach(signalName => {
  process.on(signalName, gracefulShutdown(signalName));
});

function gracefulShutdown(signalName) {
  return () => {
    console.log(`received ${signalName}, stopping server`);
    server.close(() => {
      console.log('server stopped, bye');
      // It is enough to close the server, we do not need to actually call process.exit. Once the event loop is empty,
      // Node.js will terminate the process.
    });
  };
}
