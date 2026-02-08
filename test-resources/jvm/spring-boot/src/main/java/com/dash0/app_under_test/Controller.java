package com.dash0.app_under_test;

import io.prometheus.metrics.core.metrics.Histogram;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;
import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.node.ObjectNode;

@RestController
public class Controller {

    private static final Logger logger = LoggerFactory.getLogger(RequestLoggingFilter.class);

    private static final String[] CONNECTOR_LATENCY_MILLISECONDS_LABELS = new String[]{"endpoint", "status"};
    private static final boolean SIMULATE_LATENCY =
        "true".equalsIgnoreCase(System.getenv("SIMULATE_LATENCY"));

    private final Histogram connectorLatencyMillisecondsHistogram =
        Histogram.builder()
            .name("pipeline_connector_request_duration_milliseconds")
            .help("Histogram of request durations with native histograms")
            .nativeOnly()
            .labelNames(CONNECTOR_LATENCY_MILLISECONDS_LABELS)
            .register();

    @Autowired
    private ObjectMapper mapper;

    @GetMapping("/ready")
    @ResponseStatus(code = HttpStatus.NO_CONTENT)
    public void ready() {
        long startTime = System.currentTimeMillis();
        simulateArtificialLatency("/ready", 200);
        long duration = System.currentTimeMillis() - startTime;
        connectorLatencyMillisecondsHistogram.labelValues("/ready", "success").observe(duration);
    }

    @GetMapping(path="/dash0-k8s-operator-test", produces= MediaType.APPLICATION_JSON_VALUE)
    public ObjectNode test() {
        long startTime = System.currentTimeMillis();
        simulateArtificialLatency("/dash0-k8s-operator-test", 10_000);
        ObjectNode response = mapper.createObjectNode();
        response.put("message", "We make Observability easy for every developer.");
        long duration = System.currentTimeMillis() - startTime;
        connectorLatencyMillisecondsHistogram.labelValues("/dash0-k8s-operator-test", "success").observe(duration);
        return response;
    }

    private static void simulateArtificialLatency(String endpoint, int maxLatencyMillis) {
        if (SIMULATE_LATENCY) {
            try {
                long randomLatencyMillis = Math.round(Math.random() * maxLatencyMillis);
                logger.info("simulating latency in endpoint %s: %d ms".formatted(endpoint, maxLatencyMillis));
                Thread.sleep(randomLatencyMillis);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
    }
}