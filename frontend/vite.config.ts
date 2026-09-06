import { defineConfig } from "vite";

export default defineConfig({
  plugins: [
    {
      name: "rc-style-csp-nonce",
      enforce: "pre",
      transform(source, id) {
        // rc scrollbar measurement / scroll locking do not receive ConfigProvider.csp.
        // Apply the document nonce only inside this trusted dependency's style factory;
        // do not patch document.createElement or authorize arbitrary injected styles.
        if (!id.endsWith("/@rc-component/util/es/Dom/dynamicCSS.js")) return;
        const anchor = "const styleNode = document.createElement('style');";
        if (!source.includes(anchor))
          throw new Error(
            "rc style factory changed: review CSP compatibility before upgrading",
          );
        return source.replace(
          anchor,
          `${anchor}\n  const documentNonce = document.querySelector('meta[name="csp-nonce"]')?.content;\n  if (documentNonce && documentNonce !== '__CSP_NONCE__') styleNode.nonce = documentNonce;`,
        );
      },
    },
  ],
  build: {
    rollupOptions: {
      onwarn(warning, warn) {
        // Client-only Vite app: React Server Component directives are not used.
        if (
          warning.code === "MODULE_LEVEL_DIRECTIVE" &&
          warning.message.includes('"use client"')
        )
          return;
        warn(warning);
      },
      output: {
        manualChunks(id) {
          if (id.includes("node_modules")) {
            if (/node_modules\/(react|react-dom|scheduler)\//.test(id))
              return "react-runtime";
            return "ui-vendor";
          }
        },
      },
    },
  },
});
