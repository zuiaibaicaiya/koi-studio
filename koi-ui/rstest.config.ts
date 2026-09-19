import { withRsbuildConfig } from '@rstest/adapter-rsbuild';
import { defineConfig } from '@rstest/core';

// Docs: https://rstest.rs/config/
// 复用 rsbuild.config.ts 的别名（@ → src）与 unplugin 自动引入等配置，
// 保证测试环境与构建环境的模块解析行为一致。
//
// bundleDependencies：rstest 默认把第三方依赖 external 给 Node.js 原生加载，
// 而源码进入 bundle graph，可能造成 pinia/vue 等带模块级状态的包出现双实例。
// 统一打进 bundle graph，保证测试与源码只有一份实例。
export default defineConfig({
  extends: withRsbuildConfig(),
  testEnvironment: 'happy-dom',
  setupFiles: ['./tests/rstest.setup.ts'],
  output: {
    bundleDependencies: ['pinia', 'pinia-plugin-persistedstate', 'vue'],
  },
});
