// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

'use strict';

const fs = require('node:fs/promises');
const path = require('node:path');

const debug = require('debug')('app:index');
const express = require('express');

const analyzer = require('../analyzer');
const { AppError } = require('../errors');

const router = express.Router();

const filtersDir = path.join(__dirname, '..', 'filters');
let predefinedFilters = [];
const defaultFilterName = 'show_all_resources.json';
const defaultFilter = require(path.join(filtersDir, defaultFilterName));
let currentFilterConfig = defaultFilter;
let currentPredefinedFilterName;
let sourceDirectory;
if (process.env.SOURCE_DIRECTORY) {
  sourceDirectory = process.env.SOURCE_DIRECTORY;
} else {
  sourceDirectory = path.join(__dirname, '..', '..', '..', 'e2e-test-volumes', 'otlp-sink');
}

exports.init = async (req, res, next) => {
  const allFilterFiles = await fs.readdir(filtersDir);
  predefinedFilters = [];
  await Promise.all(
    allFilterFiles.map(async fileName => {
      if (!fileName.endsWith('.json')) {
        return;
      }
      const filePath = path.join(filtersDir, fileName);
      const content = await fs.readFile(filePath, { encoding: 'utf-8' });
      let parsed;
      try {
        parsed = JSON.parse(content);
      } catch (e) {
        console.error(`cannot parse predefined filter ${filePath}`);
        return;
      }
      predefinedFilters.push(parsed);
    }),
  );

  predefinedFilters.sort((f1, f2) => {
    if (f1.name === 'show all resources' && f2.name !== 'show all resources') {
      return -1;
    }
    if (f2.name === 'show all resources' && f1.name !== 'show all resources') {
      return 1;
    }
    return f1.name.localeCompare(f2.name);
  });

  next();
};

router.get('/', async (req, res, next) => {
  const response = defaultResponse();
  try {
    const results = await analyzer.parseFilesAndApplyFilters(sourceDirectory, currentFilterConfig);
    response.results = results;
    res.render('index', response);
  } catch (err) {
    if (err instanceof AppError) {
      res.status(err.status);
      response.message = err.message;
      res.render('index', response);
    } else {
      next(err);
    }
  }
});

router.post('/', (req, res, next) => {
  if (!req.body.filter) {
    console.error('no filter');
    return res.sendStatus(400);
  }
  const newFilterRaw = req.body.filter;
  try {
    currentFilterConfig = JSON.parse(newFilterRaw);
  } catch (e) {
    res.status(400);
    return res.render('index', defaultResponseWithMessage(`cannot parse filter: ${e}`));
  }

  res.redirect('/');
});

router.post('/predefined', (req, res, next) => {
  const predefinedFilterName = req.body.predefined;
  if (predefinedFilterName === '') {
    currentPredefinedFilterName = null;
    res.redirect('/');
    return;
  }

  if (predefinedFilterName == null) {
    console.error();
    res.status(400);
    return res.render('index', defaultResponseWithMessage('no predefined filter'));
  }

  const selectedFilter = predefinedFilters.filter(f => f.name === predefinedFilterName);
  if (selectedFilter.length === 0) {
    res.status(404);
    return res.render('index', defaultResponseWithMessage(`predefined filter not found: ${predefinedFilterName}`));
  } else if (selectedFilter.length > 1) {
    res.status(400);
    return res.render('index', defaultResponseWithMessage(`duplicate predefined filters: ${predefinedFilterName}`));
  }
  currentFilterConfig = selectedFilter[0];
  currentPredefinedFilterName = currentFilterConfig.name;

  res.redirect('/');
});

router.post('/source-directory', (req, res, next) => {
  sourceDirectory = req.body['source-directory'];
  if (sourceDirectory == null || sourceDirectory == '') {
    res.status(400);
    return res.render('index', defaultResponseWithMessage('no source directory selected'));
  }

  res.redirect('/');
});

function defaultResponse() {
  return {
    sourceDirectory,
    currentFilterConfig: JSON.stringify(currentFilterConfig, null, 2),
    predefinedFilters,
    currentPredefinedFilterName,
  };
}

function defaultResponseWithMessage(message) {
  const response = defaultResponse();
  return {
    ...response,
    message,
  };
}

exports.router = router;
