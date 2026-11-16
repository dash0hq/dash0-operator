// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	migrateStagingDirName   = ".filelogoffsetsync-migrate-staging"
	migrateCommittedDirName = ".filelogoffsetsync-migrate-committed"
)

// migrateOffsets restores the offset files from the config map, like initOffsets, but only when the offset directory
// is still empty: it migrates the offsets of a collector that was running with config map based storage into a
// volume, once, and must not overwrite the offsets that the collector has written to the volume since. A config map
// that does not exist means there is nothing to migrate.
//
// Content of the config map that cannot be restored (a corrupt archive, or one without offset files) is logged and
// skipped, so that it does not keep the collector from starting on this node. Errors accessing the Kubernetes API or
// the file system are returned, so that the init container is retried.
//
// The files are restored into a staging directory inside the offset directory and synced to disk. The staging
// directory is then renamed to the committed directory, and only after that are the files moved to their final paths.
// A file is therefore never visible at its final path before it has been written completely. On start, a staging
// directory left over by an interrupted attempt is discarded, and the files in a leftover committed directory are
// moved into place.
func migrateOffsets(ctx context.Context, settings *Settings) (int, error) {
	offsetDir := settings.FileLogOffsetDirectoryPath
	stagingDir := filepath.Join(offsetDir, migrateStagingDirName)
	committedDir := filepath.Join(offsetDir, migrateCommittedDirName)

	if err := os.RemoveAll(stagingDir); err != nil {
		return 0, fmt.Errorf("cannot remove the leftover staging directory %v: %w", stagingDir, err)
	}
	if _, err := os.Stat(committedDir); err == nil {
		logger.Info("completing an interrupted migration of offset files", "directory", committedDir)
		return moveCommittedOffsetFiles(committedDir, offsetDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("cannot access the committed directory %v: %w", committedDir, err)
	}

	existingEntries, err := countOffsetDirectoryEntries(offsetDir)
	if err != nil {
		return 0, err
	}
	if existingEntries > 0 {
		logger.Info(
			"the offset directory is not empty, not migrating offset files from the config map",
			"directory", offsetDir,
			"entries", existingEntries,
		)
		return 0, nil
	}

	restoredFiles, err := restoreOffsetsIntoStagingDir(ctx, settings, stagingDir)
	if err != nil || restoredFiles == 0 {
		if removeErr := os.RemoveAll(stagingDir); removeErr != nil {
			return 0, errors.Join(err, fmt.Errorf("cannot remove the staging directory %v: %w", stagingDir, removeErr))
		}
		return 0, skipUnusableConfigMapContent(settings, err)
	}

	if err := syncDir(stagingDir); err != nil {
		return 0, err
	}
	if err := os.Rename(stagingDir, committedDir); err != nil {
		return 0, fmt.Errorf("cannot rename %v to %v: %w", stagingDir, committedDir, err)
	}
	if err := syncDir(offsetDir); err != nil {
		return 0, err
	}
	if _, err := moveCommittedOffsetFiles(committedDir, offsetDir); err != nil {
		return 0, err
	}
	return restoredFiles, nil
}

func skipUnusableConfigMapContent(settings *Settings, err error) error {
	configMap := fmt.Sprintf("%v/%v", settings.ConfigMapNamespace, settings.ConfigMapName)
	var corruptErr *corruptArchiveError
	switch {
	case apierrors.IsNotFound(err):
		logger.Info("the config map does not exist, there are no offset files to migrate", "configmap", configMap)
		return nil
	case errors.As(err, &corruptErr):
		logger.Error(
			"the offset files of this node in the config map cannot be restored; the collector will start without "+
				"them, so log records written while switching to the volume may be missed or emitted again",
			"configmap", configMap,
			"node", settings.NodeName,
			errorLabel, err,
		)
		return nil
	default:
		return err
	}
}

func countOffsetDirectoryEntries(offsetDir string) (int, error) {
	entries, err := os.ReadDir(offsetDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("cannot read the offset directory %v: %w", offsetDir, err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Name() != migrateStagingDirName && entry.Name() != migrateCommittedDirName {
			count++
		}
	}
	return count, nil
}

func restoreOffsetsIntoStagingDir(ctx context.Context, settings *Settings, stagingDir string) (int, error) {
	tr, err := readOffsetArchive(ctx, settings)
	if err != nil || tr == nil {
		return 0, err
	}
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return 0, fmt.Errorf("cannot create the staging directory %v: %w", stagingDir, err)
	}
	offsetDir := settings.FileLogOffsetDirectoryPath
	restoredFiles, err := restoreArchive(tr, func(entryName string) string {
		relativePath, err := filepath.Rel(offsetDir, filepath.FromSlash(entryName))
		if relativePath == "." {
			return ""
		}
		if err != nil || !filepath.IsLocal(relativePath) {
			logger.Warn("skipping an archive entry outside of the offset directory", "name", entryName)
			return ""
		}
		return filepath.Join(stagingDir, relativePath)
	})
	if err == nil && restoredFiles == 0 {
		return 0, &corruptArchiveError{err: errors.New("the archive contains no offset files")}
	}
	return restoredFiles, err
}

func moveCommittedOffsetFiles(committedDir string, offsetDir string) (int, error) {
	entries, err := os.ReadDir(committedDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read the committed directory %v: %w", committedDir, err)
	}
	movedEntries := 0
	for _, entry := range entries {
		source := filepath.Join(committedDir, entry.Name())
		target := filepath.Join(offsetDir, entry.Name())
		if _, err := os.Lstat(target); err == nil {
			logger.Warn("not overwriting an existing offset file with the migrated one", "name", target)
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return movedEntries, fmt.Errorf("cannot access %v: %w", target, err)
		}
		if err := os.Rename(source, target); err != nil {
			return movedEntries, fmt.Errorf("cannot move %v to %v: %w", source, target, err)
		}
		logger.Info("migrated offset file", "name", target)
		movedEntries++
	}
	if err := syncDir(offsetDir); err != nil {
		return movedEntries, err
	}
	if err := os.RemoveAll(committedDir); err != nil {
		return movedEntries, fmt.Errorf("cannot remove the committed directory %v: %w", committedDir, err)
	}
	return movedEntries, nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open directory %v: %w", path, err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	// Some file systems do not support syncing a directory.
	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) && !errors.Is(syncErr, syscall.ENOTSUP) {
		return fmt.Errorf("cannot sync directory %v: %w", path, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("cannot close directory %v: %w", path, closeErr)
	}
	return nil
}
