	public class Main {

		public static void main(String[] args) {
			String value = System.getenv("UNDEFINED_ENVIRONMENT_VARIABLE");
			if (value != null) {
				throw new RuntimeException(String.format("Unexpected value for the 'UNDEFINED_ENVIRONMENT_VARIABLE' env var: %s", value));
			}
		}
	}
