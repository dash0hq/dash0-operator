#!/usr/bin/env node --experimental-strip-types

// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0


import { promisify } from 'node:util';
import childProcess from 'node:child_process';
import { existsSync } from 'node:fs';
import { readFile, writeFile, readdir, unlink } from 'node:fs/promises';
import { basename, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import chalk from 'chalk';

import { setStartTimeBuild, storeBuildStepDuration, printTotalBuildTimeInfo } from './build-time-profiling.ts';
import { isRemoteImage, cleanupDockerImagesInstrumentationImageTests } from './util.ts';

const exec = promisify(childProcess.exec);

setStartTimeBuild();

// Change to script directory
const scriptDir = dirname(fileURLToPath(import.meta.url));
process.chdir(resolve(scriptDir, '..'));

console.log('----------------------------------------');

const allDockerPlatforms = 'linux/arm64,linux/amd64';
const testScriptDir = 'test';
const slowTestThresholdSeconds = 10;

let instrumentationImage = 'dash0-instrumentation:latest';
let testExitCode = 0;
let summary = '';

const architectures = process.env.ARCHITECTURES ? process.env.ARCHITECTURES.split(',') : [];
if (architectures.length > 0) {
  console.log('Only testing a subset of architectures:', architectures);
}

const runtimes = process.env.RUNTIMES ? process.env.RUNTIMES.split(',') : [];
if (runtimes.length > 0) {
  console.log('Only testing a subset of runtimes:', runtimes);
}

const baseImages = process.env.BASE_IMAGES ? process.env.BASE_IMAGES.split(',') : [];
if (baseImages.length > 0) {
  console.log('Only testing a subset of base images:', baseImages);
}

const testCases = process.env.TEST_CASES ? process.env.TEST_CASES.split(',') : [];
if (testCases.length > 0) {
  console.log('Only running a subset of test cases:', testCases);
}

async function buildOrPullInstrumentationImage(): void {
  const startTimeStep = Date.now();

  if (process.env.INSTRUMENTATION_IMAGE) {
    instrumentationImage = process.env.INSTRUMENTATION_IMAGE;

    if (isRemoteImage(instrumentationImage)) {
      console.log('----------------------------------------');
      console.log(`fetching instrumentation image from remote repository: ${instrumentationImage}`);
      console.log('----------------------------------------');
      await writeFile('test/.container_images_to_be_deleted_at_end', instrumentationImage + '\n', { flag: 'a' });
      await exec(`docker pull "${instrumentationImage}"`);
    } else {
      console.log('----------------------------------------');
      console.log(`using existing local instrumentation image: ${instrumentationImage}`);
      console.log('----------------------------------------');
    }
    storeBuildStepDuration('pull instrumentation image', startTimeStep);
  } else {
    console.log('----------------------------------------');
    console.log(`building multi-arch instrumentation image for platforms ${allDockerPlatforms} from local sources`);
    console.log('----------------------------------------');

    await writeFile('test/.container_images_to_be_deleted_at_end', instrumentationImage + '\n', { flag: 'a' });

    try {
      const { stdout: dockerBuildOutputStdOut, stderr: dockerBuildOutputStdErr } = await exec(
        `docker build --platform "${allDockerPlatforms}" . -t "${instrumentationImage}"`,
        { encoding: 'utf8', stdio: 'pipe' },
      );

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        if (dockerBuildOutputStdOut) {
          console.log(dockerBuildOutputStdOut);
        }
        if (dockerBuildOutputStdErr) {
          console.log(dockerBuildOutputStdErr);
        }
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      console.log(error.stdout || error.message);
      process.exit(1);
    }

    storeBuildStepDuration('build instrumentation image', startTimeStep);
  }
  console.log();
}

async function runTestsForRuntime(
  arch: string,
  dockerPlatform: string,
  runtime: string,
  imageNameTest: string,
  containerNameTestPrefix: string,
  baseImage: string,
): void {
  const testCasesDir = `${testScriptDir}/${runtime}/test-cases`;
  if (!existsSync(testCasesDir)) {
    return;
  }

  const testCaseDirs = (await readdir(testCasesDir, { withFileTypes: true }))
    .filter(dirent => dirent.isDirectory())
    .map(dirent => `${testCasesDir}/${dirent.name}/`);

  for (const testCaseDir of testCaseDirs) {
    if (testCases.length > 0) {
      const shouldRunTestCase = testCases.some(selectedTestCase => testCaseDir.includes(selectedTestCase));
      if (!shouldRunTestCase) {
        console.log(`- skipping test case ${testCaseDir}`);
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
          `Error: Test handler for runtime "${runtime}" is not implemented. Please update test-all.ts, function runTestsForRuntime.`,
        );
        process.exit(1);
    }

    // discard potential left-overs from previous test runs
    const containerName = `${containerNameTestPrefix}-${test}`;
    try {
      await exec(`docker rm -f "${containerName}"`, { stdio: 'ignore' });
    } catch {
      // ignore if container doesn't exist
    }

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
        '--name',
        containerName,
        imageNameTest,
        ...testCmd,
      ]
        .map(arg => `"${arg}"`)
        .join(' ');

      const { stdout: dockerRunOutputStdOut, stderr: dockerRunOutputStdErr } = await exec(dockerRunCmd, {
        encoding: 'utf8',
        stdio: 'pipe',
      });

      if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
        if (dockerRunOutputStdOut) {
          console.log(dockerRunOutputStdOut);
        }
        if (dockerRunOutputStdErr) {
          console.log(dockerRunOutputStdErr);
        }
      }
      console.log(chalk.green(`test case "${test}": OK`));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (error: any) {
      console.log(chalk.red(`test case "${test}": FAIL`));
      console.log(chalk.red('test command was:'));
      console.log(chalk.red(testCmd.join(' ')));
      console.log(chalk.red('test output:\n'));
      console.log(chalk.red(error.stdout || error.message));
      testExitCode = 1;
      summary += `\n${runtime}/${baseImage}\t- ${test}:\tfailed`;
    }

    const endTimeTestCase = Date.now();
    const durationTestCase = Math.floor((endTimeTestCase - startTimeTestCase) / 1000);
    if (durationTestCase > slowTestThresholdSeconds) {
      console.log(
        `! slow test case: ${imageNameTest}/${baseImage}/${test}: took ${durationTestCase} seconds, logging output:`,
      );
      // Note: dockerRunOutput would need to be captured here if needed
    }

    storeBuildStepDuration(`test case ${test}`, startTimeTestCase, arch, runtime, baseImage);
  }
}

async function runTestsForArchitecture(arch: string): void {
  let dockerPlatform: string;
  if (arch === 'arm64') {
    dockerPlatform = 'linux/arm64';
  } else if (arch === 'x86_64') {
    dockerPlatform = 'linux/amd64';
  } else {
    throw new Error(`The architecture ${arch} is not supported.`);
  }

  console.log('========================================');
  console.log(`running tests for architecture ${arch}`);
  console.log('========================================');

  const runtimeDirs = (await readdir(testScriptDir, { withFileTypes: true }))
    .filter(dirent => dirent.isDirectory())
    .map(dirent => dirent.name);

  for (const runtime of runtimeDirs) {
    const baseImagesFile = `${testScriptDir}/${runtime}/base-images`;
    const testCasesDir = `${testScriptDir}/${runtime}/test-cases`;

    if (!existsSync(baseImagesFile) || !existsSync(testCasesDir)) {
      continue;
    }

    if (runtimes.length > 0 && !runtimes.includes(runtime)) {
      console.log('----------------------------------------');
      console.log(`- skipping runtime ${runtime}`);
      continue;
    }

    console.log('----------------------------------------');
    console.log(`- runtime: '${runtime}'\n`);

    const baseImagesForRuntime = (await readFile(baseImagesFile, 'utf8'))
      .split('\n')
      .filter(line => line.trim() && !line.startsWith('#') && !line.startsWith(';'));

    for (const baseImage of baseImagesForRuntime) {
      if (baseImages.length > 0 && !baseImages.includes(baseImage)) {
        console.log('--------------------');
        console.log(`- skipping base image ${baseImage}`);
        continue;
      }

      console.log('--------------------');
      console.log(`- base image: '${baseImage}'`);

      const containerNameTestPrefix = `instrumentation-image-test-${runtime}-${arch}`;
      const imageNameTest = `instrumentation-image-test-${runtime}-${arch}:latest`;

      console.log(
        `building test image "${imageNameTest}" for ${arch}/${runtime}/${baseImage} with instrumentation image ${instrumentationImage}`,
      );

      const startTimeDockerBuild = Date.now();
      await writeFile('test/.container_images_to_be_deleted_at_end', imageNameTest + '\n', { flag: 'a' });

      try {
        const dockerBuildCmd = [
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
          stdio: 'pipe',
        });

        if (process.env.PRINT_DOCKER_OUTPUT === 'true') {
          if (dockerBuildOutputStdOut) {
            console.log(dockerBuildOutputStdOut);
          }
          if (dockerBuildOutputStdErr) {
            console.log(dockerBuildOutputStdErr);
          }
        }
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } catch (error: any) {
        console.log(error.stdout || error.message);
        process.exit(1);
      }

      storeBuildStepDuration('docker build', startTimeDockerBuild, arch, runtime, baseImage);
      await runTestsForRuntime(arch, dockerPlatform, runtime, imageNameTest, containerNameTestPrefix, baseImage);
      console.log();
    }
  }
  console.log('\n');
}

// Main execution
async function main(): Promise<void> {
  // Check Docker driver (skip in CI)
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

  for (const arch of allArchitectures) {
    if (architectures.length > 0 && !architectures.includes(arch)) {
      console.log('========================================');
      console.log(`- skipping CPU architecture ${arch}`);
      console.log('========================================');
      continue;
    }
    await runTestsForArchitecture(arch);
  }

  if (testExitCode !== 0) {
    console.log(chalk.red(`\nThere have been failing test cases:`));
    console.log(chalk.red(summary));
    console.log(chalk.red(`\nSee above for details.`));
  } else {
    console.log(chalk.green(`\nAll test cases have passed.`));
  }
  console.log('\n');

  process.exit(testExitCode);
}

await main();
