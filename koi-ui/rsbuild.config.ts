import { defineConfig, loadEnv } from '@rsbuild/core';
import { pluginVue } from '@rsbuild/plugin-vue';
import { pluginNodePolyfill } from '@rsbuild/plugin-node-polyfill';
import { electronRs } from 'electron-rs';
import Components from 'unplugin-vue-components/rspack';
import AutoImport from 'unplugin-auto-import/rspack';
import { AntdvNextResolver } from '@antdv-next/auto-import-resolver';
import { pluginWinInstaller } from './plugins/rsbuild-plugin-win-installer';

// Docs: https://rsbuild.rs/config/
export default defineConfig(({ env }) => {
  // 按 mode 加载 .env.[mode]（development 对应 8000，production 对应 5168），
  // 并将以 VITE_ 开头的变量注入 import.meta.env / process.env
  const { publicVars } = loadEnv({ mode: env, prefixes: ['PUBLIC_'] });

  return {
  plugins: [
    pluginVue(),
    electronRs({ignorePack:true}),
    pluginNodePolyfill(),
    // 一键打包前后端并生成 Windows 安装程序：npm run package:win（即 KOI_PACKAGE=1）
    // 详细配置项见 plugins/rsbuild-plugin-win-installer/README.md
    pluginWinInstaller({
      appName: 'koi-studio',
      publisher: 'koi-studio',
      backend: {
        executable: 'koi-server.exe',
        // 端口由 .env.production 的 PUBLIC_API_BASE 自动推导（5168），此处仅按需追加/覆盖
        // env: { APP_ENV: 'production', APP_DEBUG: 'false' },
        // restart: { delayMs: 3000, maxRestarts: 5, windowMs: 60000 },
        // autoStart: { enabled: true, delaySeconds: 5 },
      },
    }),
  ],
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
