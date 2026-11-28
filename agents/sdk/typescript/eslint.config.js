import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";

export default defineConfig(
  { ignores: ["dist/**"] },
  tseslint.configs.recommendedTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
  {
    // The SDK describes the provider clients by their shape and never loads them.
    files: ["src/**/*.ts"],
    rules: {
      "@typescript-eslint/no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["openai", "openai/*", "@anthropic-ai/sdk", "@anthropic-ai/sdk/*", "@flexprice/sdk", "@flexprice/sdk/*"],
              allowTypeImports: true,
              message: "Import only types from this package. The SDK must not load it at runtime.",
            },
          ],
        },
      ],
    },
  },
);
