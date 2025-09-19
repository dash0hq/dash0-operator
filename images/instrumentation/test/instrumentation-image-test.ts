#!/usr/bin/env -S node --experimental-strip-types

// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import { promisify } from 'node:util';
import childProcess, { execSync } from 'node:child_process';
import { existsSync, readFileSync, unlinkSync } from 'node:fs';
import { readdir, readFile, writeFile, unlink } from 'node:fs/promises';
import os from 'node:os';
import { basename, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { PromisePool } from '@supercharge/promise-pool';
import chalk from 'chalk';
import { parse as parseYaml } from 'yaml';

import { setStartTimeBuild, storeBuildStepDuration, printTotalBuildTimeInfo } from './build-time-profiling.ts';

const __filename = new URL(import.meta.url).pathname;

type TestImage = {
  arch: string;
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

type TestCaseProperties = {
  skip?: boolean;
  skipReason?: string;
};

const exec = promisify(childProcess.exec);

setStartTimeBuild();

const scriptDir = dirname(fileURLToPath(import.meta.url));
process.chdir(resolve(scriptDir, '..'));

const allArchitectures = ['arm64', 'x86_64'];
const allDockerPlatforms = 'linux/arm64,linux/amd64';
const testScriptDir = 'test';
const slowTestThresholdSeconds = 60;

let instrumentationImage = 'dash0-instrumentation:latest';
let failedTestCases = 0;
let skippedTestCases = 0;
let summary = '';

console.log('----------------------------------------');

const architecturesFilter = process.env.ARCHITECTURES ? process.env.ARCHITECTURES.split(',') : [];
if (architecturesFilter.length > 0) {
  if (architecturesFilter.some(arch => !allArchitectures.includes(arch))) {
    console.error(
      `Error: The ARCHITECTURES environment variable ("${architecturesFilter}") contains an unsupported architecture. Supported architectures are: ${allArchitectures.join(', ')}.`,
    );
    process.exit(1);
  }
  log('Only testing a subset of architectures:', architecturesFilter);
}

const allRuntimes = (await readdir(testScriptDir, { withFileTypes: true }))
  .filter(dirent => dirent.isDirectory())
  .filter(dirent => dirent.name !== 'node_modules')
  .map(dirent => dirent.name);
log(`Found runtime directories: ${allRuntimes.join(', ')}`);

const runtimesFilter = process.env.RUNTIMES ? process.env.RUNTIMES.split(',') : [];
if (runtimesFilter.length > 0) {
  if (runtimesFilter.some(runtime => !allRuntimes.includes(runtime))) {
    console.error(
      `Error: The RUNTIMES environment variable ("${runtimesFilter}") contains an unsupported runtime. Supported runtimes are: ${allRuntimes.join(', ')}.`,
    );
    process.exit(1);
  }
  log('Only testing a subset of runtimes:', runtimesFilter);
}

const baseImagesFilter = process.env.BASE_IMAGES ? process.env.BASE_IMAGES.split(',') : [];
if (baseImagesFilter.length > 0) {
  log('Only testing a subset of base images:', baseImagesFilter);
}

const testCaseFilter = process.env.TEST_CASES ? process.env.TEST_CASES.split(',') : [];
if (testCaseFilter.length > 0) {
  log('Only running a subset of test cases:', testCaseFilter);
}

let concurrency: number;
if (process.env.CONCURRENCY) {
  const parsedConcurrency = Number.parseInt(process.env.CONCURRENCY, 10);
  if (!Number.isNaN(parsedConcurrency) && parsedConcurrency > 0) {
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

const verbose = process.env.VERBOSE === 'true';
const suppressSkippedInfo = process.env.SUPPRESS_SKIPPED === 'true';

async function main(): Promise<void> {
  return runAllTests();
}

async function runAllTests(): Promise<void> {
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

  const testedArchitectures = allArchitectures.filter(arch => {
    if (architecturesFilter.length > 0 && !architecturesFilter.includes(arch)) {
      if (!suppressSkippedInfo) {
        log(`- skipping CPU architecture '${arch}'`);
      }
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

  if (allRunTestCaseTasks.length === 0) {
    console.error(
      `Error: No test cases found for the current filter settings (ARCHITECTURES, RUNTIMES, BASE_IMAGES, TEST_CASES, etc.).`,
    );
    process.exit(1);
  }

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

  printSummary();
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
    let dockerPlatforms = allDockerPlatforms;
    if (architecturesFilter.length > 0) {
      // If we do not run the tests for multiple CPU architectures, we do not need to spend the time to build a
      // multi-arch instrumenation image.
      dockerPlatforms = architecturesFilter.map(architectureToDockerPlatform).join(',');
    }
    log(`starting: building instrumentation image for platform(s) ${dockerPlatforms} from local sources`);
    await writeFile('test/.container_images_to_be_deleted_at_end', instrumentationImage + '\n', { flag: 'a' });
    try {
      const dockerBuildCmd = `docker build --platform "${dockerPlatforms}" . -t "${instrumentationImage}"`;
      if (verbose) {
        log(`running: ${dockerBuildCmd}`);
      }
      const { stdout: dockerBuildOutputStdOut, stderr: dockerBuildOutputStdErr } = await exec(dockerBuildCmd, {
        encoding: 'utf8',
      });
      if (verbose) {
        if (dockerBuildOutputStdOut) {
          log(dockerBuildOutputStdOut);
        }
        if (dockerBuildOutputStdErr) {
          log(dockerBuildOutputStdErr);
        }
      }
      log(`done: building instrumentation image for platform(s) ${dockerPlatforms} from local sources`);

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('build instrumentation image', startTimeStep);
  }
}

function isRemoteImage(imageName: string): boolean {
  if (!imageName) {
    throw new Error('error: mandatory argument "imageName" is missing');
  }
  return imageName.includes('/');
}

async function buildTestImagesForArchitecture(arch: string): Promise<BuildTestImagePromise[]> {
  const buildTestImageTasksPerRuntime = allRuntimes.map(runtime =>
    buildTestImagesForArchitectureAndRuntime(arch, runtime),
  );
  return (await Promise.all(buildTestImageTasksPerRuntime)).flat();
}

async function buildTestImagesForArchitectureAndRuntime(
  arch: string,
  runtime: string,
): Promise<BuildTestImagePromise[]> {
  const runtimePathPrefix = `${testScriptDir}/${runtime}`;
  const dockerFile = `${runtimePathPrefix}/Dockerfile`;
  const baseImagesFile = `${runtimePathPrefix}/base-images`;
  const testCasesDir = `${runtimePathPrefix}/test-cases`;

  if (!existsSync(dockerFile)) {
    if (!suppressSkippedInfo) {
      log(`- ${arch}: ${runtimePathPrefix} has no Dockerfile, skipping`);
    }
    return [];
  }
  if (!existsSync(baseImagesFile)) {
    if (!suppressSkippedInfo) {
      log(`- ${arch}: ${runtimePathPrefix} has no base-images file, skipping`);
    }
    return [];
  }
  if (!existsSync(testCasesDir)) {
    if (!suppressSkippedInfo) {
      log(`- ${arch}: ${runtimePathPrefix} has no test-cases directory, skipping`);
    }
    return [];
  }
  if (runtimesFilter.length > 0 && !runtimesFilter.includes(runtime)) {
    if (!suppressSkippedInfo) {
      log(`- ${arch}: skipping runtime '${runtime}'`);
    }
    return [];
  }

  const baseImagesForRuntime = (await readFile(baseImagesFile, 'utf8'))
    .split('\n')
    .filter(line => line.trim() && !line.startsWith('#') && !line.startsWith(';'));
  if (baseImagesForRuntime.length === 0) {
    if (!suppressSkippedInfo) {
      log(`- ${arch}: ${testScriptDir}/${runtime}/base-images does not list any images, skipping`);
    }
    return [];
  }

  log(`- ${arch}: creating test image build tasks for runtime: '${runtime}'`);
  const buildTasksPerBaseImage = baseImagesForRuntime.map(baseImage =>
    buildTestImageForArchitectureRuntimeAndBaseImage(arch, runtime, baseImage),
  );
  let testImages = await Promise.all(buildTasksPerBaseImage);
  testImages = testImages.filter(img => !img.skipped);
  return testImages;
}

function buildTestImageForArchitectureRuntimeAndBaseImage(
  arch: string,
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

  const baseImageStringForImageName = baseImageRun.replaceAll(':', '-').replaceAll('.', '-').replaceAll('/', '-');
  const imageNameTest = `instrumentation-image-test-${arch}-${runtime}-${baseImageStringForImageName}:latest`;
  const testImage: TestImage = {
    arch,
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
    if (!suppressSkippedInfo) {
      log(`- ${arch}/${runtime}: skipping base image ${baseImageLine}`);
    }
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
        architectureToDockerPlatform(arch),
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

      if (verbose) {
        log(`running: ${dockerBuildCmd}`);
      }
      const { stdout: dockerBuildOutputStdOut, stderr: dockerBuildOutputStdErr } = await exec(dockerBuildCmd, {
        encoding: 'utf8',
      });

      if (verbose) {
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
    if (!suppressSkippedInfo) {
      console.error(`Test case directory does not exist: ${testCasesDir}, skipping`);
    }
    return [];
  }

  let baseImageForPrefix = baseImageRun;
  if (arch.length + runtime.length + baseImageForPrefix.length > 32) {
    baseImageForPrefix = `${baseImageForPrefix.substring(0, 8)}..${baseImageForPrefix.substring(baseImageForPrefix.length - 8)}`;
  }
  const archRuntimeBaseImagePrefix = `${arch}/${runtime}/${baseImageForPrefix}`;
  const testCaseDirs = (await readdir(testCasesDir, { withFileTypes: true }))
    .filter(dirent => dirent.isDirectory())
    .map(dirent => `${testCasesDir}/${dirent.name}/`);

  const runTestCasePromises: RunTestCasePromise[] = [];
  for (const testCaseDir of testCaseDirs) {
    const testCase = basename(testCaseDir);
    if (testCaseFilter.length > 0) {
      const shouldRunTestCase = testCaseFilter.some(selectedTestCase => testCase === selectedTestCase);
      if (!shouldRunTestCase) {
        skippedTestCases++;
        if (!suppressSkippedInfo) {
          log(`${archRuntimeBaseImagePrefix.padEnd(32)}\t - skipping test case ${testCaseDir}`);
          summary += chalk.yellow(`\n${archRuntimeBaseImagePrefix.padEnd(32)}\t- ${testCaseDir}: skipped`);
        }
        continue;
      }
    }

    const startTimeTestCase = Date.now();

    let testCmd: string[];
    switch (runtime) {
      case 'c':
      case 'distroless-with-libc':
        testCmd = [`/test-cases/${testCase}/app.o`];
        break;

      case 'dotnet':
      case 'distroless-static':
        testCmd = [`/test-cases/${testCase}/app`];
        break;

      case 'jvm':
        testCmd = ['java', '-jar'];
        const systemPropsFile = `${testScriptDir}/${runtime}/test-cases/${testCase}/system.properties`;
        if (existsSync(systemPropsFile)) {
          const props = (await readFile(systemPropsFile, 'utf8')).split('\n').filter(line => line.trim());
          testCmd.push(...props);
        }
        testCmd.push('-Dotel.instrumentation.common.default-enabled=false', `/test-cases/${testCase}/app.jar`);
        break;

      case 'python':
        testCmd = ['python', `/test-cases/${testCase}/app.py`];
        break;

      case 'node':
        testCmd = ['node', `/test-cases/${testCase}/index.js`];
        break;

      default:
        console.error(
          `Error: Test handler for runtime "${runtime}" is not implemented. Please update "${__filename}", function runTestCasesForArchitectureRuntimeAndBaseImage.`,
        );
        process.exit(1);
    }
    runTestCasePromises.push({
      testImage,
      promise: createRunTestCaseTask(testImage, archRuntimeBaseImagePrefix, testCase, testCmd, startTimeTestCase),
      skipped: false,
    });
  }

  return runTestCasePromises;
}

function createRunTestCaseTask(
  testImage: TestImage,
  archRuntimeBaseImagePrefix: string,
  testCase: string,
  testCmd: string[],
  startTimeTestCase: number,
): () => Promise<void> {
  // Wrapping the actual process.exec call for "docker run" in a function enables throttling the builds with a
  // promise pool.
  return async function () {
    const {
      //
      arch,
      runtime,
      imageNameTest,
      baseImageRun,
    } = testImage;

    try {
      let dockerRunCmdArray = ['docker', 'run', '--rm', '--platform', architectureToDockerPlatform(arch)];
      const testCasePath = `${testScriptDir}/${runtime}/test-cases/${testCase}`;

      let testCaseProperties: TestCaseProperties = {};
      const testCasePropertiesFile = `${testCasePath}/.testcase.yaml`;
      if (existsSync(testCasePropertiesFile)) {
        const testCasePropertiesContent = await readFile(testCasePropertiesFile);
        try {
          testCaseProperties = parseYaml(testCasePropertiesContent.toString());
        } catch (e) {
          log(chalk.red(`Error: cannot parse ${testCasePropertiesFile}; test case won't be executed. Error: ${e}`));
          failedTestCases++;
          summary += chalk.red(
            `\nError: cannot parse ${testCasePropertiesFile}; test case has not been executed. Error: ${e}`,
          );
        }
      }

      if (testCaseProperties.skip === true) {
        // Do not count test cases skipped via .testcase.yaml as skipped for the test suite summary, that would fail
        // CI builds etc.
        if (!suppressSkippedInfo) {
          log(
            chalk.yellow(
              `${archRuntimeBaseImagePrefix.padEnd(32)}\t- skipping test case "${testCase} because of "skip: true" in ${testCasePropertiesFile}; reason: ${testCaseProperties.skipReason}"`,
            ),
          );
          summary += chalk.yellow(
            `\n${archRuntimeBaseImagePrefix.padEnd(32)}\t- ${testCase}: skipped via ${testCasePropertiesFile}`,
          );
        }
        return;
      }

      const envFile = `${testCasePath}/.env`;
      if (existsSync(envFile)) {
        dockerRunCmdArray = dockerRunCmdArray.concat(['--env-file', envFile]);
      }
      dockerRunCmdArray = dockerRunCmdArray.concat([imageNameTest, ...testCmd]);
      const dockerRunCmd = dockerRunCmdArray.map(arg => `"${arg}"`).join(' ');

      if (verbose) {
        log(`running: ${dockerRunCmdArray.join(' ')}`);
      }
      const { stdout: dockerRunOutputStdOut, stderr: dockerRunOutputStdErr } = await exec(dockerRunCmd, {
        encoding: 'utf8',
      });
      if (verbose) {
        if (dockerRunOutputStdOut) {
          log(dockerRunOutputStdOut);
        }
        if (dockerRunOutputStdErr) {
          log(dockerRunOutputStdErr);
        }
      }
      log(chalk.green(`${archRuntimeBaseImagePrefix.padEnd(32)}\t- test case "${testCase}": OK`));
      summary += chalk.green(`\n${archRuntimeBaseImagePrefix.padEnd(32)}\t- ${testCase}: OK`);

      const endTimeTestCase = Date.now();
      const durationTestCase = Math.floor((endTimeTestCase - startTimeTestCase) / 1000);
      if (durationTestCase > slowTestThresholdSeconds) {
        log(
          `! slow test case: ${imageNameTest}/${baseImageRun}/${testCase}: took ${durationTestCase} seconds, logging output:`,
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
      handleTestCaseError(`${archRuntimeBaseImagePrefix.padEnd(32)}\t`, testCase, testCmd.join(' '), [], error);
    }

    storeBuildStepDuration(`test case ${testCase}`, startTimeTestCase, arch, runtime, baseImageRun);
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function handleTestCaseError(prefix: string, testCase: string, testCmd: string, testArgs: string[], error: any) {
  let failureMode = 'FAIL';
  let failureModeSummary = 'failed';

  // Note: Detecting segfaults currently only works when triggering the test from within the container, not via docker
  // run. The reason is that the SIGSEGV signal is not propagated back from the docker run command. The segfault message
  // is also not present in stdout or stderr returned from the `await exec(testCmd...`. :-/
  if (error.signal === 'SIGSEGV') {
    failureMode = 'SEGFAULT';
    failureModeSummary = 'segfaulted';
  }

  log(
    chalk.red(
      `${prefix}- test case "${testCase}": ${failureMode}
test command was: ${testCmd} ${testArgs.join(' ')}
error:
${error}`,
    ),
  );

  failedTestCases++;
  summary += chalk.red(`\n${prefix}- ${testCase}: ${failureModeSummary}`);
}

function printSummary() {
  log('all tests have been executed\n');
  if (failedTestCases > 0) {
    console.log(chalk.red('There have been failing test cases:'));
    console.log(summary);
    console.log(chalk.red('See above for details.'));
    process.exit(failedTestCases);
  } else if (skippedTestCases > 0) {
    console.log(chalk.yellow('Some tests have been skipped:'));
    console.log(summary);
    if (!suppressSkippedInfo) {
      console.log(chalk.yellow('See above for details.'));
    } else {
      console.log(
        chalk.yellow('Detailed information about skipped test has been suppressed with SUPPRESS_SKIPPED=true.'),
      );
    }
    process.exit(skippedTestCases);
  } else {
    console.log(chalk.green(`All test cases have passed.`));
    console.log(summary);
    process.exit(0);
  }
}

function architectureToDockerPlatform(arch: string): string {
  if (arch === 'arm64') {
    return 'linux/arm64';
  } else if (arch === 'x86_64') {
    return 'linux/amd64';
  } else {
    throw new Error(`The architecture ${arch} is not supported.`);
  }
}

function cleanupDockerContainerImages(): void {
  if (process.env.DOCKER_CLEANUP_ENABLED === 'false') {
    log('[cleanup] skipping cleanup of containers and images');
    return;
  }
  if (process.env.CI) {
    log('[cleanup] skipping cleanup of containers and images on CI');
    return;
  }

  const baseDir = 'test';
  try {
    const imagesFile = `${baseDir}/.container_images_to_be_deleted_at_end`;
    const images = readFileSync(imagesFile, 'utf8')
      .split('\n')
      .map(line => line.trim())
      .filter(line => line.length > 0);

    const uniqueImages = [...new Set(images)].sort();
    for (const image of uniqueImages) {
      try {
        log(`[cleanup] removing image: ${image}`);
        execSync(`docker rmi -f "${image}"`, { stdio: 'ignore' });
      } catch {
        // Ignore errors
      }
    }

    unlinkSync(imagesFile);
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
  } catch (error) {
    // Ignore cleanup errors
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function log(message?: any, ...optionalParams: any[]): void {
  console.log(`${new Date().toLocaleTimeString()}: ${message}`, ...optionalParams);
}

await main();
