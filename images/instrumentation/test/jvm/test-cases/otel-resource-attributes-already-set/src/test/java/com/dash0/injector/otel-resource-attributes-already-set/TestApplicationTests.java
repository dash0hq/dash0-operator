package com.dash0.injector.existing_envvar_demo;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.TestPropertySource;

@SpringBootTest
@TestPropertySource(properties={"OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name,key1=value1,key2=value2"})
class TestApplicationTests {

	@Test
	void contextLoadSucceeds() {
	}

}
