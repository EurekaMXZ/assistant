import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      // Data fetching on mount legitimately sets state in effects.
      "react-hooks/set-state-in-effect": "off",
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/components/ui/**/*.{ts,tsx}", "src/lib/utils.ts"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXOpeningElement[name.name='button']",
          message: "Use the shared Button component instead of a native <button>.",
        },
        {
          selector: "ImportDeclaration[source.value='clsx']",
          message: "Import cn from @/lib/utils instead of clsx in application code.",
        },
        {
          selector: "ImportDeclaration[source.value='tailwind-merge']",
          message: "Import cn from @/lib/utils instead of tailwind-merge in application code.",
        },
      ],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
