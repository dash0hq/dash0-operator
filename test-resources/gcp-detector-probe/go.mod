module github.com/dash0hq/dash0-operator/test-resources/gcp-detector-probe

go 1.27.0

require (
	cloud.google.com/go/compute/metadata v0.9.0
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.35.0
)

require golang.org/x/sys v0.47.0 // indirect
