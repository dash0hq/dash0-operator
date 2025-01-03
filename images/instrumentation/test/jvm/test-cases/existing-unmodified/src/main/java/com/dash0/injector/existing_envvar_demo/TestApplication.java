package com.dash0.injector.existing_envvar_demo;

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
		final String value = environment.getProperty("AN_ENVIRONMENT_VARIABLE");
		if (!"value".equals(value)) {
			throw new RuntimeException(String.format("Unexpected value for the 'AN_ENVIRONMENT_VARIABLE' env var: %s", value));
		}
	}

	public static void main(String[] args) {
		SpringApplication.run(TestApplication.class, args);
	}

}
