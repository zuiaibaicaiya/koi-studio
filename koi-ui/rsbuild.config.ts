import { defineConfig, loadEnv } from '@rsbuild/core';
import { pluginVue } from '@rsbuild/plugin-vue';
import { pluginNodePolyfill } from '@rsbuild/plugin-node-polyfill';
import { electronRs } from 'electron-rs';
import Components from 'unplugin-vue-components/rspack';
import AutoImport from 'unplugin-auto-import/rspack';
import { AntdvNextResolver } from '@antdv-next/auto-import-resolver';

// Docs: https://rsbuild.rs/config/
export default defineConfig(({ env }) => {
  // 按 mode 加载 .env.[mode]（development 对应 8000，production 对应 5168），
  // 并将以 VITE_ 开头的变量注入 import.meta.env / process.env
  const { publicVars } = loadEnv({ mode: env, prefixes: ['PUBLIC_'] });

  return {
  plugins: [pluginVue(), electronRs({ignorePack:true}), pluginNodePolyfill()],
  source: {
    define: publicVars,
  },
  tools: {
    rspack: {
      plugins: [
        AutoImport({
          imports: ['vue'],
          dts: 'src/auto-imports.d.ts',
        }),
        Components({
          resolvers: [AntdvNextResolver({ resolveIcons: true })],
          dts: 'src/components.d.ts',
        }),
      ],
    },
  },
  resolve: {
    alias: {
      '@': './src',
    },
  },
  };
});
