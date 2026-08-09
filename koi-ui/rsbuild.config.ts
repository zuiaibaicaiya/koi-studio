import { defineConfig } from '@rsbuild/core';
import { pluginVue } from '@rsbuild/plugin-vue';
import { pluginNodePolyfill } from '@rsbuild/plugin-node-polyfill';
import { electronRs } from 'electron-rs';
import Components from 'unplugin-vue-components/rspack';
import AutoImport from 'unplugin-auto-import/rspack';
import { AntdvNextResolver } from '@antdv-next/auto-import-resolver';

// Docs: https://rsbuild.rs/config/
export default defineConfig({
  plugins: [pluginVue(), electronRs(), pluginNodePolyfill()],
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
});
