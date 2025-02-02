public class Main {

    public static void main(String[] args) {
        String value = System.getenv("AN_ENVIRONMENT_VARIABLE");
        if (!"value".equals(value)) {
            throw new RuntimeException(String.format("Unexpected value for the 'AN_ENVIRONMENT_VARIABLE' env var: %s", value));
        }
    }
}
