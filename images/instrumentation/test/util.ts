// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

import { execSync } from 'child_process';
import { readFileSync, unlinkSync } from 'fs';

export function isRemoteImage(imageName: string): boolean {
  if (!imageName) {
    throw new Error('error: mandatory argument "imageName" is missing');
  }
  return imageName.includes('/');
}

export function cleanupDockerContainerImages(): void {
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
export function log(message?: any, ...optionalParams: any[]): void {
  console.log(`${new Date().toLocaleTimeString()}: ${message}`, ...optionalParams);
}