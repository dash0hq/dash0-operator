// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const express = require('express');

const port = parseInt(process.env.PORT || '1207');
const app = express();

app.get('/dash0-k8s-operator-test', (req, res) => {
  console.log('processing request');
  res.json({ message: 'We make Observability easy for every developer.' });
});

app.listen(port, () => {
  console.log(`listening on port ${port}`);
});

if (process.env.TRIGGER_SELF_AND_EXIT) {
  const testId = crypto.randomUUID();

  (async function () {
    const testIdFile = process.env.TEST_ID_FILE || '/test-uuid/test.id';
    try {
      await fs.writeFile(testIdFile, testId);
      console.log(`Test ID ${testId} has been written to file ${testIdFile}.`);
    } catch (err) {
      console.error(
        `Unable to write test ID ${testId} to file ${testIdFile}, matching spans in the end-to-end test will not work.`,
        err,
      );
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
