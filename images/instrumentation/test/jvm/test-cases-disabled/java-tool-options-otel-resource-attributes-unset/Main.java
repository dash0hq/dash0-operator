import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) {
        // Here we just verify that other existing -D properties are not affected by the injector.
        verifyProperty("another.system.property", "value");

        // Since -Dotel.resource.attributes is not provided by the application under monitoring, the injector should
        // simply supply the resource attributes set by Dash0.
        verifyProperty(
                "otel.resource.attributes",
                "k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name"
        );
    }
}
