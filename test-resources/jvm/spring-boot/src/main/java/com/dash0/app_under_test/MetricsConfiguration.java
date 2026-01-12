package com.dash0.app_under_test;

import io.prometheus.metrics.exporter.servlet.jakarta.PrometheusMetricsServlet;
import org.springframework.boot.web.servlet.ServletRegistrationBean;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class MetricsConfiguration {

    @Bean
    public ServletRegistrationBean<PrometheusMetricsServlet> prometheusServlet() {
        return new ServletRegistrationBean<>(new PrometheusMetricsServlet(), "/metrics");
    }
}
