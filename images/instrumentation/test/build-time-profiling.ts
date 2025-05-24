// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

interface BuildStepEntry {
  stepLabel: string;
  arch: string;
  runtime: string;
  baseImage: string;
  duration: number;
}

let startTimeBuild: number;

const allBuildStepTimes: BuildStepEntry[] = [];

export function setStartTimeBuild(): void {
  startTimeBuild = Date.now();
}

export function storeBuildStepDuration(
  stepLabel: string,
  start: number,
  arch: string = '-',
  runtime: string = '-',
  baseImage: string = '-',
): void {
  const end = Date.now();
  // duration is in seconds
  const duration = Math.floor((end - start) / 1000);
  allBuildStepTimes.push({
    stepLabel,
    arch,
    runtime,
    baseImage,
    duration,
  });
}

export function printTotalBuildTimeInfo(): void {
  if (process.env.PRINT_BUILD_TIME_INFO !== 'true') {
    return;
  }
  if (startTimeBuild === undefined) {
    console.log('Warning: Start time not set, cannot print total build time info');
    return;
  }

  console.log();
  console.log('**build step durations (CSV)**');
  console.log('"Build Step";"Architecture";"Runtime";"Base Image";"Duration";"Duration (formatted)"');

  for (const entry of allBuildStepTimes) {
    console.log(
      `"${entry.stepLabel}";"${entry.arch}";"${entry.runtime}";"${entry.baseImage}";"${entry.duration}";"${printTime(entry.duration)}"`,
    );
  }
  console.log();

  console.log('----------------------------------------');
  console.log('**summary**');
  printBuildStepDuration('**total build time**', startTimeBuild);
}

function printBuildStepDuration(stepLabel: string, start: number): void {
  const duration = Math.floor((Date.now() - start) / 1000);
  console.log(`[build time] ${stepLabel}:\t${printTime(duration)}`);
}

function printTime(t: number): string {
  const hours = Math.floor(t / 3600);
  const minutes = Math.floor((t % 3600) / 60);
  const seconds = t % 60;
  return `${hours.toString().padStart(2, '0')}h:${minutes.toString().padStart(2, '0')}m:${seconds.toString().padStart(2, '0')}s`;
}
