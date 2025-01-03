package com.dash0.injector.spring_micrometer_otel;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@SpringBootApplication
@RestController
public class TestApplication {

	private static final Logger LOGGER = LoggerFactory.getLogger(TestApplication.class);

	@GetMapping("/")
	public String index() {
		LOGGER.info("Greetings from Spring Boot!");
		return "Greetings from Spring Boot!";
	}

	public static void main(String[] args) {
		SpringApplication.run(TestApplication.class, args);
	}

}
