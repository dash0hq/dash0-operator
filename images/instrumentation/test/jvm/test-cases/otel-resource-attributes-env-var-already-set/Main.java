import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) {
        // The injector will inject -Dotel.resource.attributes into JAVA_TOOL_OPTIONS, independent of the
        // OTEL_RESOURCE_ATTRIBUTES environment variable.
        verifyProperty(
                "otel.resource.attributes",
                "key1=value1,key2=value2,k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name"
        );
    }
}
