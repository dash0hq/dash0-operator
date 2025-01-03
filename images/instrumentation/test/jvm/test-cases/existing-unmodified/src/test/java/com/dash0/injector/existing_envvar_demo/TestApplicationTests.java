package com.dash0.injector.existing_envvar_demo;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.TestPropertySource;

@SpringBootTest
@TestPropertySource(properties={"AN_ENVIRONMENT_VARIABLE=value"})
class TestApplicationTests {

	@Test
	void contextLoadSucceeds() {
	}

}
