// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
)

type Settings struct {
	Clientset                  *kubernetes.Clientset
	NodeName                   string
	ConfigMapNamespace         string
	ConfigMapName              string
	FileLogOffsetDirectoryPath string
}

type patch struct {
	BinaryData map[string]string `json:"binaryData,omitempty"`
}

const (
	meterName = "dash0.operator.filelog_offset_synch"
)

var (
	currentValue string

	metricNamePrefix = fmt.Sprintf("%s.", meterName)

	updateSizeMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "update.compressed_size")
	updateSizeMetric     otelmetric.Int64Gauge

	updateCounterMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "update.counter")
	updateCountMetric       otelmetric.Int64Counter

	updateErrorsMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "update.errors")
	updateErrors           otelmetric.Int64Counter

	updateDurationMetricName = fmt.Sprintf("%s%s", metricNamePrefix, "update.duration")
	updateDurationMetric     otelmetric.Float64Histogram
)

// TODO Add support for sending_queue on separate exporter
// TODO Set up compaction
// TODO Set up metrics & logs
func main() {
	ctx := context.Background()

	mode := flag.String("mode", "synch",
		"if set to 'init', it will fetch the offset files from the configmap and store it to the "+
			"path stored at ${FILELOG_OFFSET_DIRECTORY_PATH}; synch mode instead will persist the offset "+
			"files at regular intervals")

	flag.Parse()

	if otherArgs := flag.Args(); len(otherArgs) > 0 {
		log.Fatalln("Unexpected arguments: " + strings.Join(otherArgs, ","))
	}

	configMapNamespace, isSet := os.LookupEnv("K8S_CONFIGMAP_NAMESPACE")
	if !isSet {
		log.Fatalln("Required env var 'K8S_CONFIGMAP_NAMESPACE' is not set")
	}

	configMapName, isSet := os.LookupEnv("K8S_CONFIGMAP_NAME")
	if !isSet {
		log.Fatalln("Required env var 'K8S_CONFIGMAP_NAME' is not set")
	}

	nodeName, isSet := os.LookupEnv("K8S_NODE_NAME")
	if !isSet {
		log.Fatalln("Required env var 'K8S_NODE_NAME' is not set")
	}

	fileLogOffsetDirectoryPath, isSet := os.LookupEnv("FILELOG_OFFSET_DIRECTORY_PATH")
	if !isSet {
		log.Fatalln("Required env var 'FILELOG_OFFSET_DIRECTORY_PATH' is not set")
	}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Cannot create the Kube API client: %v\n", err)
	}

	meter, selfMonitoringShutdownFunctions := common.InitOTelSdk(ctx, meterName, nil)
	initializeSelfMonitoringMetrics(meter)

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Cannot create the Kube API client: %v\n", err)
	}

	settings := &Settings{
		Clientset:                  clientset,
		NodeName:                   nodeName,
		ConfigMapNamespace:         configMapNamespace,
		ConfigMapName:              configMapName,
		FileLogOffsetDirectoryPath: fileLogOffsetDirectoryPath,
	}

	switch *mode {
	case "init":
		if restoredFiles, err := initOffsets(ctx, settings); err != nil {
			log.Fatalf("No offset files restored: %v\n", err)
		} else if restoredFiles == 0 {
			log.Println("No offset files restored")
		}
	case "synch":
		if err := synchOffsets(ctx, settings); err != nil {
			log.Fatalf("An error occurred while synching file offsets to configmap: %v\n", err)
		}
	}

	common.ShutDownOTelSdk(ctx, selfMonitoringShutdownFunctions)
}

func initOffsets(ctx context.Context, settings *Settings) (int, error) {
	configMap, err := settings.Clientset.CoreV1().ConfigMaps(settings.ConfigMapNamespace).Get(ctx, settings.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("cannot retrieve %v/%v config map: %w", settings.ConfigMapNamespace, settings.ConfigMapName, err)
	}

	offsetBytes, isSet := configMap.BinaryData[settings.NodeName]
	if !isSet {
		// No previous state found
		return 0, nil
	}

	gr, err := gzip.NewReader(bytes.NewReader(offsetBytes))
	if err != nil {
		return 0, fmt.Errorf("cannot uncompress '%v' field of %v/%v config map: %w", settings.NodeName, settings.ConfigMapNamespace, settings.ConfigMapName, err)
	}

	tr := tar.NewReader(gr)

	restoredFiles := 0
	for {
		// Reached the end of the tarfile
		archiveFullyRead, hasRestoredFile, err := restoreFile(tr)

		if err != nil {
			return restoredFiles, err
		}

		if hasRestoredFile {
			restoredFiles += 1
		}

		if archiveFullyRead {
			break
		}
	}

	return restoredFiles, nil
}

type IsArchiveOver bool

type HasRestoredFileFromArchive bool

func restoreFile(tr *tar.Reader) (IsArchiveOver, HasRestoredFileFromArchive, error) {
	nextHeader, err := tr.Next()

	switch {
	case err == io.EOF || nextHeader == nil:
		return true, false, nil
	case err != nil:
		return false, false, fmt.Errorf("cannot read next archive header: %w", err)
	}

	switch nextHeader.Typeflag {
	case tar.TypeDir:
		if _, err := os.Stat(nextHeader.Name); err != nil {
			if err := os.Mkdir(nextHeader.Name, 0755); err != nil {
				return false, false, fmt.Errorf("cannot create directory '%v': %w", nextHeader.Name, err)
			}
			log.Printf("Restored directory '%v'\n", nextHeader.Name)
		}
		return false, false, nil
	case tar.TypeReg:
		file, err := os.OpenFile(nextHeader.Name, os.O_CREATE|os.O_TRUNC|os.O_RDWR, os.FileMode(nextHeader.Mode))
		if err != nil {
			return false, false, fmt.Errorf("cannot create file '%v': %w", nextHeader.Name, err)
		}

		if _, err := io.Copy(file, tr); err != nil {
			return false, false, fmt.Errorf("cannot write %v bytes to file '%v': %w", nextHeader.Size, nextHeader.Name, err)
		}

		if err := file.Close(); err != nil {
			return false, false, fmt.Errorf("cannot close file '%v': %w", nextHeader.Name, err)
		}

		log.Printf("Restored file '%v' (%v bytes)\n", nextHeader.Name, nextHeader.Size)
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unexpected tar type '%v' for entry '%v' (size: %v)", nextHeader.Typeflag, nextHeader.Name, nextHeader.Size)
	}
}

func synchOffsets(ctx context.Context, settings *Settings) error {
	ticker := time.NewTicker(5 * time.Second)
	shutdown := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(shutdown, syscall.SIGTERM)

	go func() {
		for {
			select {
			case <-ticker.C:
				if err := doSynchOffsetsAndMeasure(ctx, settings); err != nil {
					log.Printf("Cannot update offset files: %v\n", err)
				}
			case <-shutdown:
				ticker.Stop()

				if err := doSynchOffsetsAndMeasure(ctx, settings); err != nil {
					log.Printf("Cannot update offset files on shutdown: %v\n", err)
				}

				done <- true
			}
		}
	}()

	<-done

	return nil
}

type OffsetSizeBytes int
type IsOffsetUpdated bool

func doSynchOffsetsAndMeasure(ctx context.Context, settings *Settings) error {
	start := time.Now()
	offsetUpdated, offsetUpdateSize, err := doSynchOffsets(settings)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("Cannot update offset files: %v\n", err)
		updateErrors.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("error.type", "CannotUpdateOffsetFiles"),
			attribute.String("error.message", err.Error()),
		))
	} else if offsetUpdated {
		updateCountMetric.Add(ctx, 1)
		updateSizeMetric.Record(ctx, int64(offsetUpdateSize))
		updateDurationMetric.Record(ctx, elapsed.Seconds())
	}

	return err
}

func doSynchOffsets(settings *Settings) (IsOffsetUpdated, OffsetSizeBytes, error) {
	var buf bytes.Buffer

	// Compress folder to tar, store bytes in configmap
	tarredFiles, err := tarFolder(settings.FileLogOffsetDirectoryPath, &buf)
	if err != nil {
		return false, -1, fmt.Errorf("cannot prepare offset files for storing: %w", err)
	}

	if tarredFiles == 0 {
		return false, -1, nil
	}

	newValue := base64.StdEncoding.EncodeToString(buf.Bytes())

	if newValue == currentValue {
		return false, -1, nil
	}

	if err := patchConfigMap(settings.Clientset, settings.NodeName, settings.ConfigMapNamespace, settings.ConfigMapName, newValue); err != nil {
		return false, -1, fmt.Errorf("cannot store offset files in configmap %v/%v: %w", settings.ConfigMapNamespace, settings.ConfigMapName, err)
	}

	currentValue = newValue
	return false, OffsetSizeBytes(len(buf.Bytes())), nil
}

func patchConfigMap(clientset *kubernetes.Clientset, nodeName string, configMapNamespace string, configMapName string, newValueBase64 string) error {
	patch := &patch{
		BinaryData: map[string]string{
			nodeName: newValueBase64,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("cannot marshal configuration map patch: %w", err)
	}

	if _, err := clientset.CoreV1().ConfigMaps(configMapNamespace).Patch(context.Background(), configMapName, types.MergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("cannot update '%v' field of configuration map  %v/%v: %w; merge patch sent: '%v'", nodeName, configMapNamespace, configMapName, err, string(patchBytes))
	}

	return nil
}

func tarFolder(filelogOffsetDirectoryPath string, writer io.Writer) (int, error) {
	gw := gzip.NewWriter(writer)
	tw := tar.NewWriter(gw)

	tarredFiles := 0
	if err := filepath.Walk(filelogOffsetDirectoryPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				// An error occurred while walking the path, terminate the walk
				return err
			}

			if hasAddedFileToArchive, err := tarFile(tw, path, info); err != nil {
				return fmt.Errorf("cannot add entry '%v' to archive: %w", path, err)
			} else if hasAddedFileToArchive {
				tarredFiles += 1
			}

			return nil
		}); err != nil {
		return tarredFiles, fmt.Errorf("an error occurred while tar-ing the offset files: %w", err)
	}

	if err := tw.Close(); err != nil {
		return tarredFiles, fmt.Errorf("cannot close the tar writer: %w", err)
	}

	if err := gw.Close(); err != nil {
		return tarredFiles, fmt.Errorf("cannot close the gzip writer: %w", err)
	}

	return tarredFiles, nil
}

type HasAddedFileToArchive bool

func tarFile(writer *tar.Writer, path string, info os.FileInfo) (HasAddedFileToArchive, error) {
	header, err := tar.FileInfoHeader(info, path)
	if err != nil {
		return false, fmt.Errorf("cannot create tar header for file '%v': %w", path, err)
	}

	// Normalize file name in header
	header.Name = filepath.ToSlash(path)

	if err := writer.WriteHeader(header); err != nil {
		return false, err
	}

	if info.IsDir() {
		return false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return false, err
	}

	if _, err := io.Copy(writer, file); err != nil {
		return false, err
	}

	if err := file.Close(); err != nil {
		return true, err
	}

	return true, nil
}

func initializeSelfMonitoringMetrics(meter otelmetric.Meter) {
	var err error

	if updateSizeMetric, err = meter.Int64Gauge(
		updateSizeMetricName,
		otelmetric.WithUnit("By"),
		otelmetric.WithDescription("The size of the compressed offset file"),
	); err != nil {
		log.Fatalf("Cannot initialize the OTLP meter for the offset file size gauge: %v", err)
	}

	if updateCountMetric, err = meter.Int64Counter(
		updateCounterMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter of how many times the synch process for filelog offsets occurs"),
	); err != nil {
		log.Fatalf("Cannot initialize the metric %s: %v", updateCounterMetricName, err)
	}

	if updateErrors, err = meter.Int64Counter(
		updateErrorsMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Error counter for failed filelog offset synch attempts"),
	); err != nil {
		log.Fatalf("Cannot initialize the metric %s: %v", updateErrorsMetricName, err)
	}

	if updateDurationMetric, err = meter.Float64Histogram(
		updateDurationMetricName,
		otelmetric.WithUnit("1s"),
		otelmetric.WithDescription("Histogram of how long it takes for the synch process for filelog offsets to complete"),
	); err != nil {
		log.Fatalf("Cannot initialize the metric %s: %v", updateDurationMetricName, err)
	}
}
