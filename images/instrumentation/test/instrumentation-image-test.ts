#!/usr/bin/env -S node --experimental-strip-types

// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import { promisify } from 'node:util';
import childProcess, { execSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { readFile, writeFile, readdir, unlink } from 'node:fs/promises';
import { readFileSync, unlinkSync } from 'fs';
import os from 'node:os';
import { basename, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { PromisePool } from '@supercharge/promise-pool';
import chalk from 'chalk';
import { program } from 'commander';

import { setStartTimeBuild, storeBuildStepDuration, printTotalBuildTimeInfo } from './build-time-profiling.ts';

const __filename = new URL(import.meta.url).pathname;

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

type TestCaseProperties = {
  skip?: boolean;
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

const testCases = process.env.TEST_CASES ? process.env.TEST_CASES.split(',') : [];
if (testCases.length > 0) {
  log('Only running a subset of test cases:', testCases);
}

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

async function main(): Promise<void> {
  program.option('--within-container');
  program.option('-r, --within-container-runtime <runtime>');
  program.parse();
  const commandLineOptions: any = program.opts();
  if (commandLineOptions.withinContainer) {
    await runTestsWithinContainer(commandLineOptions);
  } else {
    await runAllTests();
  }
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

function isRemoteImage(imageName: string): boolean {
  if (!imageName) {
    throw new Error('error: mandatory argument "imageName" is missing');
  }
  return imageName.includes('/');
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

  const buildTestImageTasksPerRuntime = allRuntimes.map(runtime =>
    buildTestImagesForArchitectureAndRuntime(arch, dockerPlatform, runtime),
  );
  return (await Promise.all(buildTestImageTasksPerRuntime)).flat();
}

async function buildTestImagesForArchitectureAndRuntime(
  arch: string,
  dockerPlatform: string,
  runtime: string,
): Promise<BuildTestImagePromise[]> {
  const runtimePathPrefix = `${testScriptDir}/${runtime}`;
  const dockerFile = `${runtimePathPrefix}/Dockerfile`;
  const baseImagesFile = `${runtimePathPrefix}/base-images`;
  const testCasesDir = `${runtimePathPrefix}/test-cases`;

  if (!existsSync(dockerFile)) {
    log(`- ${arch}: ${runtimePathPrefix} has no Dockerfile, skipping`);
    return [];
  }
  if (!existsSync(baseImagesFile)) {
    log(`- ${arch}: ${runtimePathPrefix} has no base-images file, skipping`);
    return [];
  }
  if (!existsSync(testCasesDir)) {
    log(`- ${arch}: ${runtimePathPrefix} has no test-cases directory, skipping`);
    return [];
  }
  if (runtimesFilter.length > 0 && !runtimesFilter.includes(runtime)) {
    log(`- ${arch}: skipping runtime '${runtime}'`);
    return [];
  }

  const baseImagesForRuntime = (await readFile(baseImagesFile, 'utf8'))
    .split('\n')
    .filter(line => line.trim() && !line.startsWith('#') && !line.startsWith(';'));
  if (baseImagesForRuntime.length === 0) {
    log(`- ${arch}: ${testScriptDir}/${runtime}/base-images does not list any images, skipping`);
    return [];
  }

  log(`- ${arch}: creating test image build tasks for runtime: '${runtime}'`);
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

  const baseImageStringForImageName = baseImageRun.replaceAll(':', '-').replaceAll('.', '-').replaceAll('/', '-');
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
  if (arch.length + runtime.length + baseImageForPrefix.length > 32) {
    baseImageForPrefix = `${baseImageForPrefix.substring(0, 8)}..${baseImageForPrefix.substring(baseImageForPrefix.length - 8)}`;
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
    const testCase = basename(testCaseDir);

    let testCmd: string[];
    switch (runtime) {
      case 'c':
        testCmd = [`/test-cases/${testCase}/app.o`];
        break;

      case 'dotnet':
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

      case 'node':
        testCmd = ['node', `/test-cases/${testCase}`];
        break;

      default:
        console.error(
          `Error: Test handler for runtime "${runtime}" is not implemented. Please update "${__filename}", function runTestCasesForArchitectureRuntimeAndBaseImage.`,
        );
        process.exit(1);
    }
    runTestCasePromises.push({
      testImage,
      promise: createRunTestCaseTask(testImage, prefix, testCase, testCmd, startTimeTestCase),
      skipped: false,
    });
  }

  return runTestCasePromises;
}

function createRunTestCaseTask(
  testImage: TestImage,
  prefix: string,
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
      dockerPlatform,
      runtime,
      imageNameTest,
      baseImageRun,
    } = testImage;

    try {
      let dockerRunCmdArray = ['docker', 'run', '--rm', '--platform', dockerPlatform];
      const testCasePath = `${testScriptDir}/${runtime}/test-cases/${testCase}`;

      let testCaseProperties: TestCaseProperties = {};
      const testCasePropertiesFile = `${testCasePath}/.testcase.json`;
      if (existsSync(testCasePropertiesFile)) {
        const testCasePropertiesContent = await readFile(testCasePropertiesFile);
        try {
          testCaseProperties = JSON.parse(testCasePropertiesContent.toString());
        } catch (e) {
          log(chalk.red(`Error: cannot parse ${testCasePropertiesFile}, ignoring.`));
        }
      }

      if (testCaseProperties.skip === true) {
        log(chalk.yellow(`${prefix.padEnd(32)}\t- test case "${testCase}": SKIPPED`));
        skippedTestCases++;
        summary += chalk.yellow(`\n${prefix.padEnd(32)}\t- ${testCase}: skipped`);
        return;
      }

      const envFile = `${testCasePath}/.env`;
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
      log(chalk.green(`${prefix.padEnd(32)}\t- test case "${testCase}": OK`));
      summary += chalk.green(`\n${prefix.padEnd(32)}\t- ${testCase}: OK`);

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
      log(
        chalk.red(
          `${prefix.padEnd(32)}\t- test case "${testCase}": FAIL
test command was:
${testCmd.join(' ')}
error: ${error}`,
        ),
      );
      failedTestCases++;
      summary += chalk.red(`\n${prefix.padEnd(32)}\t- ${testCase}: failed`);
    }

    storeBuildStepDuration(`test case ${testCase}`, startTimeTestCase, arch, runtime, baseImageRun);
  };
}

async function runTestsWithinContainer(commandLineOptions: any): Promise<void> {
  // Note that this only actually works with a subset of the test cases, in particular, the instrumentation assets
  // (Dash0 Node.js OTel distribution, Java OTel SDK, ...) are not present.

  if (!commandLineOptions.withinContainerRuntime) {
    console.error(
      chalk.red(
        'The runtime to test (c, jvm, node, dotnet, ...) has to be specified via --within-container-runtime when --within-container is used.',
      ),
    );
    process.exit(1);
  }
  const runtime = commandLineOptions.withinContainerRuntime;
  if (runtime !== 'jvm') {
    console.error(chalk.red('Currently only the runtime "jvm" is supported with --within-container.'));
    process.exit(1);
  }

  log('compiling the injector (zig build)');
  try {
    await exec('zig build --prominent-compile-errors --summary none', { cwd: 'injector' });
  } catch (error) {
    // @ts-ignore
    console.error(chalk.red('Compiling the injector failed:\n', error.stderr || error.message || error));
    process.exit(1);
  }

  switch (runtime) {
    case 'jvm':
      await runTestsWithinContainerJvm();
      break;
    default:
      console.error(`runtime ${runtime} not supported for --within-container-runtime`);
      process.exit(1);
  }

  printSummary();
}

async function runTestsWithinContainerJvm(): Promise<void> {
  log('compiling jvm-test-utils');
  try {
    await exec('javac src/com/dash0/injector/testutils/*.java', { cwd: 'test/jvm/jvm-test-utils' });
  } catch (error) {
    // @ts-ignore
    console.error(chalk.red('Compiling the injector failed:\n', error.stderr || error.message || error));
    process.exit(1);
  }

  const testCases = await readdir('test/jvm/test-cases', { withFileTypes: true });
  for (const testCaseDir of testCases) {
    if (!testCaseDir.isDirectory()) {
      continue;
    }
    await runJvmTestCaseWithinContainer(testCaseDir.name);
  }
}

async function runJvmTestCaseWithinContainer(testCase: string) {
  const cwd = `test/jvm/test-cases/${testCase}`;

  let testCaseProperties: TestCaseProperties = {};
  const testCasePropertiesFile = `${cwd}/.testcase.json`;
  if (existsSync(testCasePropertiesFile)) {
    const testCasePropertiesContent = await readFile(testCasePropertiesFile);
    try {
      testCaseProperties = JSON.parse(testCasePropertiesContent.toString());
    } catch (e) {
      log(chalk.red(`Error: cannot parse ${testCasePropertiesFile}, ignoring.`));
    }
  }

  if (testCaseProperties.skip === true) {
    log(chalk.yellow(`- test case "${testCase}": SKIPPED`));
    skippedTestCases++;
    summary += chalk.yellow(`\n- ${testCase}: skipped`);
    return;
  }

  execSync('cp -R ../../jvm-test-utils/src/* .', {
    cwd,
    stdio: 'inherit',
  });
  execSync('javac Main.java', {
    cwd,
    stdio: 'inherit',
  });
  execSync('jar --create --file app.jar --manifest MANIFEST.MF -C . .', {
    cwd,
    stdio: 'inherit',
  });
  const environmentFile = readFileSync(`${cwd}/.env`, 'utf8');
  const envForTest = environmentFile
    //
    .split('\n')
    .filter(line => !line.startsWith('#'))
    .map(line => line.trim())
    .filter(line => line.length > 0)
    .reduce((env: Record<string, string>, line) => {
      const parts: string[] = line.split('=');
      if (parts.length < 2) {
        return env;
      } else if (parts.length === 2) {
        env[parts[0]] = parts[1];
        return env;
      } else {
        // for env vars with '=' characters in the value, like OTEL_RESOURCE_ATTRIBUTES=key1=value,key2=value
        env[parts[0]] = parts.slice(1).join('=');
        return env;
      }
    }, {});

  const testCmd = 'java -jar -Dotel.instrumentation.common.default-enabled=false app.jar';
  try {
    await exec(testCmd, {
      cwd,
      env: {
        ...process.env,
        ...envForTest,
        LD_PRELOAD: '../../../../injector/dash0_injector.so',
      },
    });
    log(chalk.green(`- test case "${testCase}": OK`));
    summary += chalk.green(`\n- ${testCase}: OK`);
  } catch (error) {
    log(
      chalk.red(
        `- test case "${testCase}": FAIL
test command was:
${testCmd}
error: ${error}`,
      ),
    );
    failedTestCases++;
    summary += chalk.red(`\n- ${testCase}: failed`);
  }
}

function printSummary() {
  log('all tests have been executed\n');
  if (failedTestCases > 0) {
    console.log(chalk.red('There have been failing test cases:'));
    console.log(summary);
    console.log(chalk.red('See above for details.'));
    process.exit(failedTestCases);
  } else if (skippedTestCases > 0) {
    console.log(chalk.yellow('There are tests that are marked with skip: true in their .testcase.json file:'));
    console.log(summary);
    console.log(chalk.yellow('See above for details.'));
    process.exit(skippedTestCases);
  } else {
    console.log(chalk.green(`All test cases have passed.`));
    console.log(summary);
    process.exit(0);
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
