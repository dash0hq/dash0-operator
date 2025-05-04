public class Main {

    public static void main(String[] args) {
        String value = System.getProperty("another.system.property");
        if (!"value".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the '-Danother.system.property' system property: %s", value));
        }
        value = System.getProperty("otel.resource.attributes");
        if (!"k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the '-Dotel.resource.attributes' system property: %s", value));
        }
    }
}
