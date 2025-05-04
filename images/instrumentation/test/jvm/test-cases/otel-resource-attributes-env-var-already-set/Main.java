public class Main {

    public static void main(String[] args) {
        String value = System.getenv("OTEL_RESOURCE_ATTRIBUTES");
        if (!"key1=value1,key2=value2".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the 'OTEL_RESOURCE_ATTRIBUTES' env var: %s", value));
        }
    }
}
