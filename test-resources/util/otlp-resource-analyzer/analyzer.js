// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

'use strict';

const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const util = require('node:util');

const { AppError } = require('./errors');

const debug = require('debug')('app:analyzer');

const sortPrefixes = [
  'process.',
  'k8s.container.',
  'k8s.pod.',
  'k8s.replicaset',
  'k8s.job.',
  'k8s.cronjob.',
  'k8s.daemonset.',
  'k8s.deployment',
  'k8s.statefulset.',
  'k8s.node.',
];

const lexicographically = (k1, k2) => k1.localeCompare(k2);

exports.parseFilesAndApplyFilters = async function parseFilesAndApplyFilters(sourceDirectory, filterConfig) {
  const allResources = {};

  let files;
  try {
    files = await fs.readdir(sourceDirectory);
  } catch (e) {
    if (e.code === 'ENOENT') {
      throw new AppError(`Directory "${sourceDirectory}" does not exist.`, 404);
    }
  }
  await Promise.all(
    files.map(async fileName => {
      const fileNameParts = fileName.split('.');
      const signalType = fileNameParts[0];
      const filePath = path.join(sourceDirectory, fileName);
      const content = await fs.readFile(filePath, { encoding: 'utf-8' });
      let lines = content.split(os.EOL);
      lines.forEach(processNdJsonLine.bind(null, allResources, signalType));
    }),
  );

  const allKeys = Object.keys(allResources).sort(lexicographically);
  const matchingResources = Object.values(allResources)
    .filter(resourceWrapper => {
      return matchesFilter(filterConfig, resourceWrapper);
    })
    .sort((rw1, rw2) => {
      const k1 = Object.keys(rw1.resource);
      const k2 = Object.keys(rw2.resource);

      for (let i = 0; i < sortPrefixes.length; i++) {
        const sortPrefix = sortPrefixes[i];
        const r1HasAttribute = k1.filter(k => k.startsWith(sortPrefix)).length > 0;
        const r2HasAttribute = k2.filter(k => k.startsWith(sortPrefix)).length > 0;
        if (r1HasAttribute && !r2HasAttribute) {
          return 1;
        } else if (!r1HasAttribute && r2HasAttribute) {
          return -1;
        }
      }
      return rw1.resourceId.localeCompare(rw2.resourceId);
    });

  debug('\n\n## Matching Resources\n');
  matchingResources.forEach(resourceWrapper => {
    debug(util.inspect(resourceWrapper));
  });
  debug('\n');
  debug('## Summary\n');
  debug(`- found ${allKeys.length} resources in total`);
  debug(`- found ${matchingResources.length} resources matching filter ${util.inspect(filterConfig)}:`);
  debug('\n');
  debug('\n');

  return {
    totalCount: allKeys.length,
    matchingResourcesCount: matchingResources.length,
    matchingResources,
  };
};

function processNdJsonLine(allResources, signalType, line) {
  line = line.trim();
  if (line.length === 0) {
    // skip empty lines
    return;
  }
  let parsedLine;
  try {
    parsedLine = JSON.parse(line);
  } catch (e) {
    console.error(`cannot parse line for signal type ${signalType}: ${line}`);
    return;
  }

  processOtlpObject(allResources, signalType, parsedLine);
}

function processOtlpObject(allResources, signalType, otlpObject) {
  const conf = getOtlpParserConfig(signalType, otlpObject);
  const resourceAttribute = conf.resourceAttribute;
  const mainList = otlpObject[resourceAttribute];
  if (mainList == null) {
    throw new Error(`object with signal type ${signalType} has no main list`);
  }
  const attributesInRootObject = Object.keys(otlpObject).filter(k => k !== resourceAttribute);
  if (attributesInRootObject.length > 1) {
    throw new Error(
      `object with signal type ${signalType} has unexpected attributes: ${attributesInRootObject.join(', ')}`,
    );
  }

  processMainList(allResources, signalType, mainList, conf);
}

function getOtlpParserConfig(signalType, otlpObject) {
  switch (signalType) {
    case 'traces':
      return {
        resourceAttribute: 'resourceSpans',
        scopeAttribute: 'scopeSpans',
      };
    case 'metrics':
      return {
        resourceAttribute: 'resourceMetrics',
        scopeAttribute: 'scopeMetrics',
      };
    case 'logs':
      return {
        resourceAttribute: 'resourceLogs',
        scopeAttribute: 'scopeLogs',
      };

    default:
      throw new Error(`unknown signal type: ${signalType}: ${util.inspect(otlpObject, { depth: 2 }).substr(0, 100)}`);
  }
}

function processMainList(allResources, signalType, mainList, conf) {
  const scopeAttribute = conf.scopeAttribute;
  for (let i = 0; i < mainList.length; i++) {
    const mainItem = mainList[i];
    const resourceWrapper = processResource(allResources, signalType, mainItem.resource);
    const scopeList = mainItem[scopeAttribute];
    if (scopeList) {
      for (let j = 0; j < scopeList.length; j++) {
        const scopeWrapper = scopeList[j];
        const scopeName = scopeWrapper.scope?.name;
        if (scopeName) {
          resourceWrapper.scopeNames.add(scopeName);
        }
      }
    }
  }
}

function processResource(allResources, signalType, resource) {
  if (resource == null) {
    throw new Error(`no resource`);
  }
  const resourceAttributes = resource.attributes;
  if (resourceAttributes == null) {
    throw new Error(`no resource attributes`);
  }
  const resourceRepresentation = {};
  resourceAttributes.forEach(attr => {
    resourceRepresentation[attr.key] = unwrapValue(attr, attr.value);
  });

  const resourceId = computeResourceId(resourceRepresentation);
  const existingResourceWrapper = allResources[resourceId];
  if (existingResourceWrapper) {
    existingResourceWrapper.seen++;
    existingResourceWrapper.fromSignalTypes.add(signalType);
    return existingResourceWrapper;
  } else {
    const fromSignalTypes = new Set();
    fromSignalTypes.add(signalType);
    const newResourceWrapper = {
      resourceId,
      seen: 1,
      resource: resourceRepresentation,
      fromSignalTypes,
      scopeNames: new Set(),
    };
    allResources[resourceId] = newResourceWrapper;
    return newResourceWrapper;
  }
}

function unwrapValue(attr, value) {
  if (value.stringValue != null) {
    return value.stringValue;
  } else if (value.intValue != null) {
    return value.intValue;
  } else if (value.arrayValue != null) {
    return value.arrayValue.values.map(unwrapValue.bind(null, attr));
  }
  throw new Error(`cannot unwrap value ${util.inspect(attr)}`);
}

function computeResourceId(resourceRepresentation) {
  let objectKey = '';
  const keys = Object.keys(resourceRepresentation).sort(lexicographically);
  keys.forEach(k => {
    const val = resourceRepresentation[k];
    objectKey += `#${k}:${JSON.stringify(val)}#`;
  });
  return objectKey;
}

function matchesFilter(filter, resourceWrapper) {
  const resourceRepresentation = resourceWrapper.resource;
  const operator = filter.op;
  const filterKey = filter.key;
  const resourceVal = filterKey != null && resourceRepresentation[filterKey];
  if (operator === 'and') {
    if (filter.filters == null) {
      throw new AppError(`missing filter.filters: ${util.inspect(filter)}`);
    }
    for (let i = 0; i < filter.filters.length; i++) {
      if (!matchesFilter(filter.filters[i], resourceWrapper)) {
        return false;
      }
    }
    return true;
  } else if (operator === 'or') {
    if (filter.filters == null) {
      throw new AppError(`missing filter.filters: ${util.inspect(filter)}`);
    }
    for (let i = 0; i < filter.filters.length; i++) {
      if (matchesFilter(filter.filters[i], resourceWrapper)) {
        return true;
      }
    }
  } else if (operator === 'is-set') {
    if (filterKey == null) {
      throw new AppError(`missing filter.key: ${util.inspect(filter)}`);
    }
    return resourceVal != null;
  } else if (operator === 'is-not-set') {
    if (filterKey == null) {
      throw new AppError(`missing filter.key: ${util.inspect(filter)}`);
    }
    return resourceVal == null;
  } else if (operator === 'value-contains') {
    if (filter.regex == null) {
      throw new AppError(`missing filter.regex: ${util.inspect(filter)}`);
    }
    return new RegExp(filter.regex).test(resourceVal);
  } else if (operator === 'value-contains-not') {
    if (filter.regex == null) {
      throw new AppError(`missing filter.regex: ${util.inspect(filter)}`);
    }
    return !new RegExp(filter.regex).test(resourceVal);
  } else if (operator === 'scope-contains') {
    if (filter.regex == null) {
      throw new AppError(`missing filter.regex: ${util.inspect(filter)}`);
    }
    const scopeNames = Array.from(resourceWrapper.scopeNames);
    const exp = new RegExp(filter.regex);
    return scopeNames.filter(sn => exp.test(sn)).length > 0;
  } else if (operator === 'scope-contains-not') {
    if (filter.regex == null) {
      throw new AppError(`missing filter.regex: ${util.inspect(filter)}`);
    }
    const scopeNames = Array.from(resourceWrapper.scopeNames);
    const exp = new RegExp(filter.regex);
    return scopeNames.filter(sn => exp.test(sn)).length === 0;
  } else {
    throw new AppError(`filter not supported ${util.inspect(filter)}`);
  }
}
