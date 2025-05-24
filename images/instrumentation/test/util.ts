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

function cleanupDockerContainersAndImages(baseDir: string): void {
  if (process.env.DOCKER_CLEANUP_ENABLED === 'false') {
    console.log('[cleanup] skipping cleanup of containers and images');
    return;
  }

  if (!baseDir) {
    throw new Error('error: mandatory argument "baseDir" is missing');
  }

  try {
    const imagesFile = `${baseDir}/.container_images_to_be_deleted_at_end`;
    const images = readFileSync(imagesFile, 'utf8')
      .split('\n')
      .map(line => line.trim())
      .filter(line => line.length > 0);

    const uniqueImages = [...new Set(images)].sort();
    for (const image of uniqueImages) {
      try {
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

export function cleanupDockerImagesInstrumentationImageTests(): void {
  cleanupDockerContainersAndImages('test');
}
