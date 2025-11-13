// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	ginRouter := gin.New()
	ginRouter.Use(
		gin.LoggerWithWriter(
			gin.DefaultWriter,
			// do not write /ready checks to Gin's access log
			"/ready",
		),
		gin.Recovery(),
	)
	_ = ginRouter.SetTrustedProxies(nil)

	config := newConfigurationFromEnvironment()
	log.Printf("Configuration: %v", config)
	routes := newRoutes(config)
	routes.defineRoutes(ginRouter)

	log.Printf("Starting server on port %s", config.Port)
	if err := ginRouter.Run(":" + config.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
