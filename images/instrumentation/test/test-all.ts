#!/usr/bin/env node --experimental-strip-types

// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import { promisify } from 'node:util';
import childProcess from 'node:child_process';
import { existsSync } from 'node:fs';
import { readFile, writeFile, readdir, unlink } from 'node:fs/promises';
import os from 'node:os';
import { basename, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import chalk from 'chalk';
import { PromisePool } from '@supercharge/promise-pool';

import { setStartTimeBuild, storeBuildStepDuration, printTotalBuildTimeInfo } from './build-time-profiling.ts';
import { isRemoteImage, cleanupDockerImagesInstrumentationImageTests } from './util.ts';

type TestImage = {
  arch: string;
  dockerPlatform: string;
  runtime: string;
  imageNameTest: string;
  baseImage: string;
};

type BuildTestImagePromise = {
  testImage: TestImage;
  promise?: () => Promise<void>;
  skipped: boolean;
};

type RunTestCasePromise = {
  testImage: TestImage;
  promise?: () => Promise<void>;
  skipped: boolean;
};

const exec = promisify(childProcess.exec);

console.log('----------------------------------------');

let concurrency: number;
if (process.env.CONCURRENCY) {
  const parsedConcurrency = parseInt(process.env.CONCURRENCY, 10);
  if (!isNaN(parsedConcurrency) && parsedConcurrency > 0) {
    concurrency = parsedConcurrency;
    log(`Using concurrency from environment variable CONCURRENCY: ${concurrency}`);
  } else {
    concurrency = os.cpus().length;
    console.error(
      `Error: Cannot parse the value of the environment variable CONCURRENCY (${process.env.CONCURRENCY}) to a number. Using the default concurrency (number of available CPUs: ${concurrency}).`,
    );
  }
} else {
  concurrency = os.cpus().length;
  log(`Using the default concurrency (number of available CPUs: ${concurrency}).`);
}

setStartTimeBuild();

// Change to script directory
const scriptDir = dirname(fileURLToPath(import.meta.url));
process.chdir(resolve(scriptDir, '..'));

const allDockerPlatforms = 'linux/arm64,linux/amd64';
const testScriptDir = 'test';
const slowTestThresholdSeconds = 60;

let instrumentationImage = 'dash0-instrumentation:latest';
let testExitCode = 0;
let summary = '';

const architectures = process.env.ARCHITECTURES ? process.env.ARCHITECTURES.split(',') : [];
if (architectures.length > 0) {
  log('Only testing a subset of architectures:', architectures);
}

const runtimes = process.env.RUNTIMES ? process.env.RUNTIMES.split(',') : [];
if (runtimes.length > 0) {
  log('Only testing a subset of runtimes:', runtimes);
}

const baseImages = process.env.BASE_IMAGES ? process.env.BASE_IMAGES.split(',') : [];
if (baseImages.length > 0) {
  log('Only testing a subset of base images:', baseImages);
}

const testCases = process.env.TEST_CASES ? process.env.TEST_CASES.split(',') : [];
if (testCases.length > 0) {
  log('Only running a subset of test cases:', testCases);
}

async function main(): Promise<void> {
  log('starting instrumentation image tests');
  if (process.env.CI !== 'true') {
    try {
      let { stdout: dockerDriver } = await exec('docker info -f "{{ .DriverStatus }}"', { encoding: 'utf8' });
      dockerDriver = dockerDriver.trim();
      if (!dockerDriver.includes('io.containerd.')) {
        console.error(
          `Error: This script requires that the containerd image store is enabled for Docker, since the script needs to build and use multi-arch images locally. Your driver is ${dockerDriver}. Please see https://docs.docker.com/desktop/containerd/#enable-the-containerd-image-store for instructions on enabling the containerd image store.`,
        );
        process.exit(1);
      }
    } catch (error) {
      console.error('Error checking Docker driver status:', error);
      process.exit(1);
    }
  }

  // setup cleanup file
  try {
    // remove old cleanup file, in case it exists
    await unlink('test/.container_images_to_be_deleted_at_end');
  } catch {
    // ignore
  }
  await writeFile('test/.container_images_to_be_deleted_at_end', '');

  // run cleanup on exit
  process.on('exit', () => {
    cleanupDockerImagesInstrumentationImageTests();
    printTotalBuildTimeInfo();
  });

  await buildOrPullInstrumentationImage();

  const allArchitectures = ['arm64', 'x86_64'];

  const testedArchitectures = allArchitectures.filter(arch => {
    if (architectures.length > 0 && !architectures.includes(arch)) {
      log(`- skipping CPU architecture '${arch}'`);
      return false;
    }
    log(`- creating test image build tasks for CPU architecture '${arch}'`);
    return true;
  });

  const createBuildTaskPromises = testedArchitectures.map(arch => buildTestImagesForArchitecture(arch));
  const allTestImageBuildTasks = (await Promise.all(createBuildTaskPromises)).flat();

  log(`building ${allTestImageBuildTasks.length} test images`);
  await PromisePool.withConcurrency(concurrency)
    .for(allTestImageBuildTasks)
    .handleError(error => {
      // If the docker build fails, we call process.exit(1) in the catch in createBuildTestImageTask immediately, so we
      // actually do not get here. This error handler is only for cases where something goes wrong outside the
      // try-catch.
      log(`building a test image has failed`);
      throw error;
    })
    .onTaskFinished((_, pool) => {
      log(
        `build task progress: ${pool.processedCount()}/${allTestImageBuildTasks.length} (${pool
          .processedPercentage()
          .toLocaleString('en-US', { maximumFractionDigits: 1 })}%)`,
      );
    })
    .process(buildTestImagePromise => buildTestImagePromise.promise!());

  log(`all ${allTestImageBuildTasks.length} test images have been built, starting to run test cases now`);

  const allTestImages = allTestImageBuildTasks.map(task => task.testImage);

  const createRunTestCasesTaskPromises = allTestImages.map(runTestCasesForArchitectureRuntimeAndBaseImage);
  const allRunTestCaseTasks = (await Promise.all(createRunTestCasesTaskPromises)).flat().filter(t => !t.skipped);

  log(`running ${allRunTestCaseTasks.length} test cases`);
  await PromisePool.withConcurrency(concurrency)
    .for(allRunTestCaseTasks)
    .handleError(error => {
      // If the docker run fails, we record that as a failed test case and continue. This error handler is only for
      // cases where something goes wrong outside the try-catch, which should basically never happen. We stop execution
      // in immediately if it does, though.
      log(`running a test case has crashed`);
      throw error;
    })
    .onTaskFinished((_, pool) => {
      if (pool.processedCount() % 10 === 0 || pool.processedCount() === allRunTestCaseTasks.length) {
        log(
          `test case progress: ${pool.processedCount()}/${allRunTestCaseTasks.length} (${pool
            .processedPercentage()
            .toLocaleString('en-US', { maximumFractionDigits: 1 })}%)`,
        );
      }
    })
    .process(runTestCasePromise => runTestCasePromise.promise!());

  if (testExitCode !== 0) {
    console.log(
      chalk.red(`\nThere have been failing test cases:
${summary}
\nSee above for details.\n`),
    );
  } else {
    log(chalk.green(`All test cases have passed.`));
  }

  process.exit(testExitCode);
}

async function buildOrPullInstrumentationImage(): Promise<void> {
  const startTimeStep = Date.now();

  if (process.env.INSTRUMENTATION_IMAGE) {
    instrumentationImage = process.env.INSTRUMENTATION_IMAGE;

    if (isRemoteImage(instrumentationImage)) {
      log(`fetching instrumentation image from remote repository: ${instrumentationImage}`);
      await writeFile('test/.container_images_to_be_deleted_at_end', instrumentationImage + '\n', { flag: 'a' });
      await exec(`docker pull "${instrumentationImage}"`);
    } else {
      log(`using existing local instrumentation image: ${instrumentationImage}`);
    }
    storeBuildStepDuration('pull instrumentation image', startTimeStep);
  } else {
    log(`starting: building multi-arch instrumentation image for platforms ${allDockerPlatforms} from local sources`);

    await writeFile('test/.container_images_to_be_deleted_at_end', instrumentationImage + '\n', { flag: 'a' });

    try {
      const { stdout: dockerBuildOutputStdOut, stderr: dockerBuildOutputStdErr } = await exec(
        `docker build --platform "${allDockerPlatforms}" . -t "${instrumentationImage}"`,
        { encoding: 'utf8' },
      );

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        if (dockerBuildOutputStdOut) {
          log(dockerBuildOutputStdOut);
        }
        if (dockerBuildOutputStdErr) {
          log(dockerBuildOutputStdErr);
        }
      }
      log(`done: building multi-arch instrumentation image for platforms ${allDockerPlatforms} from local sources`);

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('build instrumentation image', startTimeStep);
  }
  console.log();
}

async function buildTestImagesForArchitecture(arch: string): Promise<BuildTestImagePromise[]> {
  let dockerPlatform: string;
  if (arch === 'arm64') {
    dockerPlatform = 'linux/arm64';
  } else if (arch === 'x86_64') {
    dockerPlatform = 'linux/amd64';
  } else {
    throw new Error(`The architecture ${arch} is not supported.`);
  }

  const runtimeDirs = (await readdir(testScriptDir, { withFileTypes: true }))
    .filter(dirent => dirent.isDirectory())
    .map(dirent => dirent.name);

  const buildTestImageTasksPerRuntime = runtimeDirs.map(runtime =>
    buildTestImagesForArchitectureAndRuntime(arch, dockerPlatform, runtime),
  );
  return (await Promise.all(buildTestImageTasksPerRuntime)).flat();
}

async function buildTestImagesForArchitectureAndRuntime(
  arch: string,
  dockerPlatform: string,
  runtime: string,
): Promise<BuildTestImagePromise[]> {
  const baseImagesFile = `${testScriptDir}/${runtime}/base-images`;
  const testCasesDir = `${testScriptDir}/${runtime}/test-cases`;

  if (!existsSync(baseImagesFile) || !existsSync(testCasesDir)) {
    return [];
  }

  if (runtimes.length > 0 && !runtimes.includes(runtime)) {
    log(`- ${arch}: skipping runtime '${runtime}'`);
    return [];
  }

  log(`- ${arch}: creating test image build tasks for runtime: '${runtime}'`);

  const baseImagesForRuntime = (await readFile(baseImagesFile, 'utf8'))
    .split('\n')
    .filter(line => line.trim() && !line.startsWith('#') && !line.startsWith(';'));

  const buildTasksPerBaseImage = baseImagesForRuntime.map(baseImage =>
    buildTestImageForArchitectureRuntimeAndBaseImage(arch, dockerPlatform, runtime, baseImage),
  );
  let testImages = await Promise.all(buildTasksPerBaseImage);
  testImages = testImages.filter(img => !img.skipped);
  return testImages;
}

function buildTestImageForArchitectureRuntimeAndBaseImage(
  arch: string,
  dockerPlatform: string,
  runtime: string,
  baseImage: string,
): BuildTestImagePromise {
  const baseImageForDockerNames = baseImage.replaceAll(':', '-');
  const imageNameTest = `instrumentation-image-test-${arch}-${runtime}-${baseImageForDockerNames}:latest`;

  const testImage: TestImage = {
    arch,
    dockerPlatform,
    runtime,
    imageNameTest,
    baseImage,
  };
  if (baseImages.length > 0 && !baseImages.includes(baseImage)) {
    log(`- ${arch}/${runtime}: skipping base image ${baseImage}`);
    return {
      testImage,
      skipped: true,
    };
  }

  const buildTestImagePromise: BuildTestImagePromise = {
    testImage,
    promise: createBuildTestImageTask(testImage),
    skipped: false,
  };
  return buildTestImagePromise;
}

function createBuildTestImageTask(testImage: TestImage): () => Promise<void> {
  // Wrapping the actual process.exec call for "docker build" in a function enables throttling the builds with a
  // promise pool.
  return async function () {
    const {
      //
      arch,
      dockerPlatform,
      runtime,
      imageNameTest,
      baseImage,
    } = testImage;

    const startTimeDockerBuild = Date.now();
    await writeFile('test/.container_images_to_be_deleted_at_end', imageNameTest + '\n', { flag: 'a' });
    log(
      `starting: building test image "${testImage.imageNameTest}" for ${testImage.arch}/${testImage.runtime}/${testImage.baseImage} with instrumentation image ${instrumentationImage}`,
    );
    let dockerBuildCmd;
    try {
      dockerBuildCmd = [
        'docker',
        'build',
        '--platform',
        dockerPlatform,
        '--build-arg',
        `instrumentation_image=${instrumentationImage}`,
        '--build-arg',
        `base_image=${baseImage}`,
        `${testScriptDir}/${runtime}`,
        '-t',
        imageNameTest,
      ]
        .map(arg => `"${arg}"`)
        .join(' ');

      const { stdout: dockerBuildOutputStdOut, stderr: dockerBuildOutputStdErr } = await exec(dockerBuildCmd, {
        encoding: 'utf8',
      });

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        if (dockerBuildOutputStdOut) {
          log(dockerBuildOutputStdOut);
        }
        if (dockerBuildOutputStdErr) {
          log(dockerBuildOutputStdErr);
        }
      }
      log(
        `done: building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImage} with instrumentation image ${instrumentationImage}`,
      );

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(
        `! error: building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImage} with instrumentation image ${instrumentationImage} has failed, docker build command was\n${dockerBuildCmd}`,
      );
      log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('docker build', startTimeDockerBuild, arch, runtime, baseImage);
  };
}

async function runTestCasesForArchitectureRuntimeAndBaseImage(testImage: TestImage): Promise<RunTestCasePromise[]> {
  const {
    //
    arch,
    runtime,
    baseImage,
  } = testImage;

  const testCasesDir = `${testScriptDir}/${runtime}/test-cases`;
  if (!existsSync(testCasesDir)) {
    return [];
  }

  const prefix = `${arch}/${runtime}/${baseImage}`;
  const testCaseDirs = (await readdir(testCasesDir, { withFileTypes: true }))
    .filter(dirent => dirent.isDirectory())
    .map(dirent => `${testCasesDir}/${dirent.name}/`);

  const runTestCasePromises: RunTestCasePromise[] = [];
  for (const testCaseDir of testCaseDirs) {
    if (testCases.length > 0) {
      const shouldRunTestCase = testCases.some(selectedTestCase => testCaseDir.includes(selectedTestCase));
      if (!shouldRunTestCase) {
        log(`- ${prefix}: skipping test case ${testCaseDir}`);
        continue;
      }
    }

    const startTimeTestCase = Date.now();
    const test = basename(testCaseDir);

    let testCmd: string[];
    switch (runtime) {
      case 'jvm':
        testCmd = ['java', '-jar'];
        const systemPropsFile = `${testScriptDir}/${runtime}/test-cases/${test}/system.properties`;
        if (existsSync(systemPropsFile)) {
          const props = (await readFile(systemPropsFile, 'utf8')).split('\n').filter(line => line.trim());
          testCmd.push(...props);
        }
        testCmd.push('-Dotel.instrumentation.common.default-enabled=false', `/test-cases/${test}/app.jar`);
        break;

      case 'node':
        testCmd = ['node', `/test-cases/${test}`];
        break;

      case 'c':
        testCmd = [`/test-cases/${test}/app.o`];
        break;

      default:
        console.error(
          `Error: Test handler for runtime "${runtime}" is not implemented. Please update test-all.ts, function runTestCasesForArchitectureRuntimeAndBaseImage.`,
        );
        process.exit(1);
    }
    runTestCasePromises.push({
      testImage,
      promise: createRunTestCaseTask(testImage, prefix, test, testCmd, startTimeTestCase),
      skipped: false,
    });
  }

  return runTestCasePromises;
}

function createRunTestCaseTask(
  testImage: TestImage,
  prefix: string,
  test: string,
  testCmd: string[],
  startTimeTestCase: number,
): () => Promise<void> {
  // Wrapping the actual process.exec call for "docker run" in a function enables throttling the builds with a
  // promise pool.
  return async function () {
    const {
      //
      arch,
      dockerPlatform,
      runtime,
      imageNameTest,
      baseImage,
    } = testImage;

    try {
      const envFile = `${testScriptDir}/${runtime}/test-cases/${test}/.env`;

      const dockerRunCmd = [
        'docker',
        'run',
        '--rm',
        '--platform',
        dockerPlatform,
        '--env-file',
        envFile,
        imageNameTest,
        ...testCmd,
      ]
        .map(arg => `"${arg}"`)
        .join(' ');

      const { stdout: dockerRunOutputStdOut, stderr: dockerRunOutputStdErr } = await exec(dockerRunCmd, {
        encoding: 'utf8',
      });

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        if (dockerRunOutputStdOut) {
          log(dockerRunOutputStdOut);
        }
        if (dockerRunOutputStdErr) {
          log(dockerRunOutputStdErr);
        }
      }
      log(chalk.green(`${prefix.padEnd(32)}\t- test case "${test}": OK`));

      const endTimeTestCase = Date.now();
      const durationTestCase = Math.floor((endTimeTestCase - startTimeTestCase) / 1000);
      if (durationTestCase > slowTestThresholdSeconds) {
        log(
          `! slow test case: ${imageNameTest}/${baseImage}/${test}: took ${durationTestCase} seconds, logging output:`,
        );
        if (dockerRunOutputStdOut) {
          log(dockerRunOutputStdOut);
        }
        if (dockerRunOutputStdErr) {
          log(dockerRunOutputStdErr);
        }
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(
        chalk.red(
          `${prefix.padEnd(32)}\t- test case "${test}": FAIL
test command was:
${testCmd.join(' ')}
${error}`,
        ),
      );
      testExitCode = 1;
      summary += `\n${prefix}\t- ${test}:\tfailed`;
    }

    storeBuildStepDuration(`test case ${test}`, startTimeTestCase, arch, runtime, baseImage);
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function log(message?: any, ...optionalParams: any[]): void {
  console.log(`${new Date().toLocaleTimeString()}: ${message}`, ...optionalParams);
}

await main();
