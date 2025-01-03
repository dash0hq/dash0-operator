package com.dash0.injector.spring_micrometer_otel;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.TestPropertySource;

@SpringBootTest
@TestPropertySource(properties = {"spring.sleuth.enabled=false"})
class TestApplicationTests {

	@Test
	void contextLoads() {
	}

}
