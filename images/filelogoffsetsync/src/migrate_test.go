// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNodeName                 = "node-1"
	testConfigMapNamespace       = "dash0-system"
	testConfigMapName            = "dash0-operator-filelogoffsets-cm"
	offsetFileName               = "receiver_filelog_"
	selfMonitoringOffsetFileName = "receiver_filelog_selfmonitoring"
)

var (
	offsetFileContent               = []byte("offsets of the file_log receiver")
	selfMonitoringOffsetFileContent = []byte("offsets of the file_log/selfmonitoring receiver")
)

func TestMain(m *testing.M) {
	logger = slog.New(slog.DiscardHandler)
	os.Exit(m.Run())
}

func TestMigrateRestoresOffsetsIntoEmptyDirectory(t *testing.T) {
	settings := newTestSettings(t)
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 2)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, defaultOffsetFiles())
}

func TestMigrateSkipsNonEmptyDirectory(t *testing.T) {
	settings := newTestSettings(t)
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())
	collectorFiles := map[string][]byte{offsetFileName: []byte("offsets written by the collector")}
	writeOffsetFiles(t, settings.FileLogOffsetDirectoryPath, collectorFiles)

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 0)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, collectorFiles)
}

func TestMigrateWithoutConfigMap(t *testing.T) {
	settings := newTestSettings(t)

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 0)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, nil)
}

func TestMigrateWithoutOffsetsForThisNode(t *testing.T) {
	settings := newTestSettings(t)
	createConfigMap(t, settings, map[string][]byte{"other-node": {}})

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 0)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, nil)
}

func TestMigrateDiscardsLeftoverStagingDirectory(t *testing.T) {
	settings := newTestSettings(t)
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())
	writeOffsetFiles(
		t,
		filepath.Join(settings.FileLogOffsetDirectoryPath, migrateStagingDirName),
		map[string][]byte{offsetFileName: offsetFileContent[:5]},
	)

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 2)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, defaultOffsetFiles())
}

func TestMigrateCompletesInterruptedMoveOfCommittedFiles(t *testing.T) {
	settings := newTestSettings(t)
	offsetDir := settings.FileLogOffsetDirectoryPath
	writeOffsetFiles(t, offsetDir, map[string][]byte{offsetFileName: offsetFileContent})
	writeOffsetFiles(t, filepath.Join(offsetDir, migrateCommittedDirName), map[string][]byte{
		offsetFileName:               []byte("stale copy of a file that has already been moved"),
		selfMonitoringOffsetFileName: selfMonitoringOffsetFileContent,
	})

	restoredFiles, err := migrateOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 1)
	requireOffsetFiles(t, offsetDir, defaultOffsetFiles())
}

func TestMigrateSkipsUnusableArchive(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		corrupt func(t *testing.T, offsetDir string, archive []byte) []byte
	}{
		{
			name: "not a gzip stream",
			corrupt: func(_ *testing.T, _ string, _ []byte) []byte {
				return []byte("not an archive")
			},
		},
		{
			name: "archive ends within the content of a file",
			corrupt: func(_ *testing.T, _ string, archive []byte) []byte {
				return archive[:len(archive)/2]
			},
		},
		{
			name: "archive ends within the header of the second file",
			corrupt: func(t *testing.T, offsetDir string, _ []byte) []byte {
				tarBytes := tarWithOneFile(t, filepath.Join(offsetDir, offsetFileName))
				tarBytes.Write(bytes.Repeat([]byte{'x'}, 100))
				return gzipBytes(t, tarBytes.Bytes())
			},
		},
		{
			name: "archive without offset files in the offset directory",
			corrupt: func(t *testing.T, _ string, _ []byte) []byte {
				tarBytes := tarWithOneFile(t, filepath.Join(t.TempDir(), offsetFileName))
				tarBytes.Write(make([]byte, 1024))
				return gzipBytes(t, tarBytes.Bytes())
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			settings := newTestSettings(t)
			offsetDir := settings.FileLogOffsetDirectoryPath
			offsetFiles := map[string][]byte{
				offsetFileName:               offsetFileContent,
				selfMonitoringOffsetFileName: randomBytes(t, 256*1024),
			}
			archive := storeOffsetsInConfigMap(t, settings, offsetFiles)
			updateConfigMap(t, settings, testCase.corrupt(t, offsetDir, archive))

			restoredFiles, err := migrateOffsets(context.Background(), settings)

			requireNoError(t, err)
			requireRestoredFiles(t, restoredFiles, 0)
			requireOffsetFiles(t, offsetDir, nil)

			updateConfigMap(t, settings, archive)
			restoredFiles, err = migrateOffsets(context.Background(), settings)

			requireNoError(t, err)
			requireRestoredFiles(t, restoredFiles, 2)
			requireOffsetFiles(t, offsetDir, offsetFiles)
		})
	}
}

func TestMigrateFailsWhenTheConfigMapCannotBeRead(t *testing.T) {
	settings := newTestSettings(t)
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())
	settings.Clientset.(*fake.Clientset).PrependReactor(
		"get",
		"configmaps",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewServiceUnavailable("the API server is unavailable")
		},
	)

	_, err := migrateOffsets(context.Background(), settings)

	if !apierrors.IsServiceUnavailable(err) {
		t.Fatalf("expected the API error to be returned, got %v", err)
	}
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, nil)
}

func TestMigrateFailsWhenTheOffsetDirectoryIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not apply to root")
	}
	settings := newTestSettings(t)
	offsetDir := settings.FileLogOffsetDirectoryPath
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())
	if err := os.Chmod(offsetDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(offsetDir, 0755) })

	_, err := migrateOffsets(context.Background(), settings)

	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("expected a permission error, got %v", err)
	}
	requireOffsetFiles(t, offsetDir, nil)
}

func TestInitRestoresOffsetsToTheArchivedPaths(t *testing.T) {
	settings := newTestSettings(t)
	storeOffsetsInConfigMap(t, settings, defaultOffsetFiles())

	restoredFiles, err := initOffsets(context.Background(), settings)

	requireNoError(t, err)
	requireRestoredFiles(t, restoredFiles, 2)
	requireOffsetFiles(t, settings.FileLogOffsetDirectoryPath, defaultOffsetFiles())
}

func newTestSettings(t *testing.T) *Settings {
	return &Settings{
		Clientset:                  fake.NewClientset(),
		NodeName:                   testNodeName,
		ConfigMapNamespace:         testConfigMapNamespace,
		ConfigMapName:              testConfigMapName,
		FileLogOffsetDirectoryPath: t.TempDir(),
	}
}

func defaultOffsetFiles() map[string][]byte {
	return map[string][]byte{
		offsetFileName:               offsetFileContent,
		selfMonitoringOffsetFileName: selfMonitoringOffsetFileContent,
	}
}

// storeOffsetsInConfigMap stores the given offset files in the config map the same way the sync mode does, and then
// removes them from the offset directory again. It returns the archive stored in the config map.
func storeOffsetsInConfigMap(t *testing.T, settings *Settings, files map[string][]byte) []byte {
	t.Helper()
	createConfigMap(t, settings, nil)
	writeOffsetFiles(t, settings.FileLogOffsetDirectoryPath, files)
	currentValue = ""
	if _, _, err := doSyncOffsets(settings); err != nil {
		t.Fatalf("cannot store the offset files in the config map: %v", err)
	}
	for name := range files {
		if err := os.Remove(filepath.Join(settings.FileLogOffsetDirectoryPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	return getConfigMap(t, settings).BinaryData[settings.NodeName]
}

func createConfigMap(t *testing.T, settings *Settings, binaryData map[string][]byte) {
	t.Helper()
	if _, err := settings.Clientset.CoreV1().ConfigMaps(settings.ConfigMapNamespace).Create(
		context.Background(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: settings.ConfigMapNamespace, Name: settings.ConfigMapName},
			BinaryData: binaryData,
		},
		metav1.CreateOptions{},
	); err != nil {
		t.Fatal(err)
	}
}

func getConfigMap(t *testing.T, settings *Settings) *corev1.ConfigMap {
	t.Helper()
	configMap, err := settings.Clientset.CoreV1().ConfigMaps(settings.ConfigMapNamespace).Get(
		context.Background(), settings.ConfigMapName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return configMap
}

func updateConfigMap(t *testing.T, settings *Settings, archive []byte) {
	t.Helper()
	configMap := getConfigMap(t, settings)
	configMap.BinaryData[settings.NodeName] = archive
	if _, err := settings.Clientset.CoreV1().ConfigMaps(settings.ConfigMapNamespace).Update(
		context.Background(), configMap, metav1.UpdateOptions{},
	); err != nil {
		t.Fatal(err)
	}
}

// tarWithOneFile returns an unterminated tar stream with a single file, to which a test can append arbitrary bytes.
func tarWithOneFile(t *testing.T, path string) *bytes.Buffer {
	t.Helper()
	var tarBytes bytes.Buffer
	tw := tar.NewWriter(&tarBytes)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.ToSlash(path),
		Mode:     0600,
		Size:     int64(len(offsetFileContent)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(offsetFileContent); err != nil {
		t.Fatal(err)
	}
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	return &tarBytes
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func randomBytes(t *testing.T, size int) []byte {
	t.Helper()
	content := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, content); err != nil {
		t.Fatal(err)
	}
	return content
}

func writeOffsetFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// requireOffsetFiles checks that the offset directory contains exactly the given files with the given content, and in
// particular no leftover staging or committed directory.
func requireOffsetFiles(t *testing.T, offsetDir string, files map[string][]byte) {
	t.Helper()
	entries, err := os.ReadDir(offsetDir)
	if err != nil {
		t.Fatal(err)
	}
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		actualNames = append(actualNames, entry.Name())
	}
	expectedNames := make([]string, 0, len(files))
	for name := range files {
		expectedNames = append(expectedNames, name)
	}
	slices.Sort(expectedNames)
	if !slices.Equal(actualNames, expectedNames) {
		t.Fatalf("expected the offset directory to contain %v, but it contains %v", expectedNames, actualNames)
	}
	for name, expectedContent := range files {
		actualContent, err := os.ReadFile(filepath.Join(offsetDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actualContent, expectedContent) {
			t.Fatalf("unexpected content of %v: %q", name, truncate(actualContent))
		}
	}
}

func requireRestoredFiles(t *testing.T, actual int, expected int) {
	t.Helper()
	if actual != expected {
		t.Fatalf("expected %v restored files, got %v", expected, actual)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func truncate(content []byte) []byte {
	if len(content) > 64 {
		return content[:64]
	}
	return content
}
