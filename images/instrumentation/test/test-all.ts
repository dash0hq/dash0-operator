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
import { log, isRemoteImage, cleanupDockerContainerImages } from './util.ts';

type TestImage = {
  arch: string;
  dockerPlatform: string;
  runtime: string;
  imageNameTest: string;
  baseImageBuild: string;
  baseImageRun: string;
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

const architecturesFilter = process.env.ARCHITECTURES ? process.env.ARCHITECTURES.split(',') : [];
if (architecturesFilter.length > 0) {
  log('Only testing a subset of architectures:', architecturesFilter);
}

const runtimesFilter = process.env.RUNTIMES ? process.env.RUNTIMES.split(',') : [];
if (runtimesFilter.length > 0) {
  log('Only testing a subset of runtimes:', runtimesFilter);
}

const baseImagesFilter = process.env.BASE_IMAGES ? process.env.BASE_IMAGES.split(',') : [];
if (baseImagesFilter.length > 0) {
  log('Only testing a subset of base images:', baseImagesFilter);
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
    cleanupDockerContainerImages();
    printTotalBuildTimeInfo();
  });

  await buildOrPullInstrumentationImage();

  const allArchitectures = ['arm64', 'x86_64'];

  const testedArchitectures = allArchitectures.filter(arch => {
    if (architecturesFilter.length > 0 && !architecturesFilter.includes(arch)) {
      log(`- skipping CPU architecture '${arch}'`);
      return false;
    }
    log(`- creating test image build tasks for CPU architecture '${arch}'`);
    return true;
  });

  const createBuildTaskPromises = testedArchitectures.map(arch => buildTestImagesForArchitecture(arch));
  const allTestImageBuildTasks = (await Promise.all(createBuildTaskPromises)).flat();

  if (allTestImageBuildTasks.length == 0) {
    console.error(
      `Error: No test cases found for the current filter settings (ARCHITECTURES, RUNTIMES, BASE_IMAGES, etc.).`,
    );
    process.exit(1);
  }
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
    log(
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
      const dockerBuildCmd = `docker build --platform "${allDockerPlatforms}" . -t "${instrumentationImage}"`;
      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        log(`running: ${dockerBuildCmd}`);
      }
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
      log(`done: building multi-arch instrumentation image for platforms ${allDockerPlatforms} from local sources`);

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('build instrumentation image', startTimeStep);
  }
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
    .filter(dirent => dirent.name !== 'node_modules')
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

  if (!existsSync(baseImagesFile)) {
    log(`- ${arch}: ${testScriptDir}/${runtime} has no base-images file, skipping`);
    return [];
  }
  if (!existsSync(testCasesDir)) {
    log(`- ${arch}: ${testScriptDir}/${runtime} has no test-cases directory, skipping`);
    return [];
  }

  if (runtimesFilter.length > 0 && !runtimesFilter.includes(runtime)) {
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
  baseImageLine: string,
): BuildTestImagePromise {
  let baseImageBuild: string = baseImageLine;
  let baseImageRun: string = baseImageLine;
  if (baseImageLine.includes(',')) {
    const images = baseImageLine.split(',');
    if (images.length < 2 || images.length > 2) {
      console.error(`Error: cannot parse base image line format: ${baseImageLine}.`);
      process.exit(1);
    }
    baseImageBuild = images[0];
    baseImageRun = images[1];
  }

  const baseImageStringForImageName = baseImageRun
      .replaceAll(':', '-')
      .replaceAll('.', '-')
      .replaceAll('/', '-');
  const imageNameTest = `instrumentation-image-test-${arch}-${runtime}-${baseImageStringForImageName}:latest`;
  const testImage: TestImage = {
    arch,
    dockerPlatform,
    runtime,
    imageNameTest,
    baseImageBuild,
    baseImageRun,
  };

  if (
    baseImagesFilter.length > 0 &&
    !baseImagesFilter.includes(baseImageBuild) &&
    !baseImagesFilter.includes(baseImageRun)
  ) {
    log(`- ${arch}/${runtime}: skipping base image ${baseImageLine}`);
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
      baseImageBuild,
      baseImageRun,
    } = testImage;

    const startTimeDockerBuild = Date.now();
    await writeFile('test/.container_images_to_be_deleted_at_end', imageNameTest + '\n', { flag: 'a' });
    log(
      `starting: building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImageBuild} with instrumentation image ${instrumentationImage}`,
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
        // Simple Dockerfiles only use one base image to build and run the app under test (if there is even a build
        // step involved), we arbitrarily pass base_image=baseImageRun to the Docker build. The base-images file for
        // these runtimes should only contain one image name per line, not a comma-separated list of two images, thus
        // baseImageBuild === baseImageRun.
        '--build-arg',
        `base_image=${baseImageRun}`,
        // More elaborate Dockerfiles (like for .NET) may use two base images, one for building the app and one for
        // running it, we pass base_image_build and base_image_run to the Docker build. The base-images file for
        // these runtimes should have a comma-separated list of two images per line.
        '--build-arg',
        `base_image_build=${baseImageBuild}`,
        '--build-arg',
        `base_image_run=${baseImageRun}`,
        `${testScriptDir}/${runtime}`,
        '-t',
        imageNameTest,
      ]
        .map(arg => `"${arg}"`)
        .join(' ');

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        log(`running: ${dockerBuildCmd}`);
      }
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
        `done: building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImageBuild} with instrumentation image ${instrumentationImage}`,
      );

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(
        `! error: building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImageBuild} with instrumentation image ${instrumentationImage} has failed, docker build command was\n${dockerBuildCmd}`,
      );
      log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('docker build', startTimeDockerBuild, arch, runtime, baseImageBuild);
  };
}

async function runTestCasesForArchitectureRuntimeAndBaseImage(testImage: TestImage): Promise<RunTestCasePromise[]> {
  const {
    //
    arch,
    runtime,
    baseImageRun,
  } = testImage;

  const testCasesDir = `${testScriptDir}/${runtime}/test-cases`;
  if (!existsSync(testCasesDir)) {
    console.error(`Test case directory does not exist: ${testCasesDir}, skipping`);
    return [];
  }

  let baseImageForPrefix = baseImageRun;
  if ((arch.length + runtime.length + baseImageForPrefix.length) > 32) {
    baseImageForPrefix = `${baseImageForPrefix.substring(0, 8)}..${baseImageForPrefix.substring(baseImageForPrefix.length-8)}`;
  }
  const prefix = `${arch}/${runtime}/${baseImageForPrefix}`;
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
      case 'c':
        testCmd = [`/test-cases/${test}/app.o`];
        break;

      case 'dotnet':
        testCmd = [`/test-cases/${test}/app`];
        break;

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
      baseImageRun,
    } = testImage;

    try {
      let dockerRunCmdArray = ['docker', 'run', '--rm', '--platform', dockerPlatform];
      const envFile = `${testScriptDir}/${runtime}/test-cases/${test}/.env`;
      if (existsSync(envFile)) {
        dockerRunCmdArray = dockerRunCmdArray.concat(['--env-file', envFile]);
      }
      dockerRunCmdArray = dockerRunCmdArray.concat([imageNameTest, ...testCmd]);
      const dockerRunCmd = dockerRunCmdArray.map(arg => `"${arg}"`).join(' ');

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        log(`running: ${dockerRunCmd}`);
      }
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
          `! slow test case: ${imageNameTest}/${baseImageRun}/${test}: took ${durationTestCase} seconds, logging output:`,
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
error: ${error}`,
        ),
      );
      testExitCode = 1;
      summary += `\n${prefix}\t- ${test}:\tfailed`;
    }

    storeBuildStepDuration(`test case ${test}`, startTimeTestCase, arch, runtime, baseImageRun);
  };
}

await main();
