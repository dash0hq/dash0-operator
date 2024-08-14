// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log"
	"os"
)

var (
	configMapName              string
	nodeName                   string
	filelogOffsetDirectoryPath string
)

func main() {
	if configMapName, hasConfigMapName := os.LookupEnv("K8S_CONFIGMAP_NAME"); !hasConfigMapName {
		log.Fatalln("Required env var 'K8S_CONFIGMAP_NAME' is not set")
	}

	if nodeName, hasNodeName := os.LookupEnv("K8S_NODE_NAME"); !hasNodeName {
		log.Fatalln("Required env var 'K8S_NODE_NAME' is not set")
	}

	if filelogOffsetDirectoryPath, hasDirPath := os.LookupEnv("FILELOG_OFFSET_DIRECTORY_PATH"); !hasDirPath {
		log.Fatalln("Required env var 'FILELOG_OFFSET_DIRECTORY_PATH' is not set")
	}

}
