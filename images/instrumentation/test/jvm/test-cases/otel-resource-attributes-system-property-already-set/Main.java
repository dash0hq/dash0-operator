public class Main {

    public static void main(String[] args) {
        String value = System.getProperty("another.system.property");
        if (!"value".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the '-Danother.system.property' system property: %s", value));
        }
        value = System.getProperty("otel.resource.attributes");
        if (!"key1=value1,key2=value2".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the '-Dotel.resource.attributes' system property: %s", value));
        }
    }
}
