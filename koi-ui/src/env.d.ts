/// <reference types="@rsbuild/core/types" />

interface ImportMetaEnv {
  /** 后端接口基地址，例如 http://127.0.0.1:8000 */
  readonly VITE_API_BASE?: string;
  /** 实时转写 Socket.IO 服务地址，缺省时复用 VITE_API_BASE */
  readonly VITE_SOCKET_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue';

  const component: DefineComponent<object, object, unknown>;
  export default component;
}
