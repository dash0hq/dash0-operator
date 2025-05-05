import static com.dash0.injector.testutils.TestUtils.*;

public class Main {

    public static void main(String[] args) {
        verifyEnvVar("AN_ENVIRONMENT_VARIABLE", "value");
    }
}
