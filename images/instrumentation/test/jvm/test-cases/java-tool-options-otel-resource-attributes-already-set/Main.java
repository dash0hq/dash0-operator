import static com.dash0.injector.testutils.TestUtils.*;

// TODO test cases:
// test case
// original JAVA_TOOL_OPTIONS: is not set                                      | add -javaagent and otel resource attribs
// original JAVA_TOOL_OPTIONS: has -javaagent                                  | stand down, do not inject our -javaagent, nor resource attributes
// original JAVA_TOOL_OPTIONS: no -javaagent, no -Dotel.resource.attributes    | inject -javaagent and -Dotel.resource.attributes
// original JAVA_TOOL_OPTIONS: no -javaagent, has a -Dotel.resource.attributes | add -javaagent, merge resource attributes
// ^^^
// all of the above, but with -D properties instead? Or vice versa?

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
