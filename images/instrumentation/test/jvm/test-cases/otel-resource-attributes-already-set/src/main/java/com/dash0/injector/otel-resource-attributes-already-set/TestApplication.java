package com.dash0.injector.existing_envvar_demo;

import java.util.Optional;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.event.EventListener;
import org.springframework.core.env.Environment;

@SpringBootApplication
public class TestApplication {

	@Autowired
	private Environment environment;

	@EventListener(ApplicationReadyEvent.class)
	public void checkEnvVar() {
		final String value = environment.getProperty("OTEL_RESOURCE_ATTRIBUTES");
		if (!Optional.of(value).orElse("").contains("key1=value1,key2=value2")) {
			throw new RuntimeException(String.format("Unexpected value for the 'OTEL_RESOURCE_ATTRIBUTES' env var: %s", value));
		}
	}

	public static void main(String[] args) {
		SpringApplication.run(TestApplication.class, args);
	}

}
