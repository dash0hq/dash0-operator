import static com.dash0.injector.testutils.TestUtils.*;

public class Main {
    public static void main(String[] args) {
        // Here we just verify that other existing -D properties are not affected by the injector.
        verifyProperty("another.system.property", "value");

        // The injector will merge the existing key-value pairs from -Dotel.resource.attributes with "our" key-value
        // pairs.
        verifyProperty(
          "otel.resource.attributes",
          "key1=value1,key2=value2,k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name"
        );
    }
}
