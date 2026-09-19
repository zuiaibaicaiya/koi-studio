import { onBeforeUnmount, onMounted, ref } from 'vue';
import presenterApi from '../services/presenter';

export interface UsePresenterWindowOptions {
  /** 生成本屏路由的 href（已去掉 hash 模式的前导 '#'） */
  buildHref: () => string;
  /** 打开第二屏失败的提示回调 */
  onError?: (message: string) => void;
}

/**
 * 第二屏投屏窗口管理封装。
 *
 * 这里只负责窗口的打开/关闭与按钮状态同步；转写内容由第二屏自行建立
 * Socket.IO 连接、以 viewer 角色加入会议观众频道订阅，不经过主进程 IPC 转发。
 * 组件卸载时自动关闭投屏窗口并解除状态订阅。
 */
export function usePresenterWindow(options: UsePresenterWindowOptions) {
  /** 第二屏投屏窗口是否已打开 */
  const presentOpen = ref(false);
  let disposeState: (() => void) | undefined;

  async function open() {
    try {
      await presenterApi.open(options.buildHref());
      presentOpen.value = true;
    } catch (err) {
      options.onError?.((err as Error)?.message || '打开第二屏失败');
    }
  }

  async function close() {
    await presenterApi.close();
    presentOpen.value = false;
  }

  async function toggle() {
    if (presentOpen.value) {
      await close();
      return;
    }
    await open();
  }

  onMounted(() => {
    // 第二屏可能在其窗口内被单独关闭，订阅状态保证按钮文案同步
    disposeState = presenterApi.onStateChange((state) => {
      presentOpen.value = state.open;
    });
    presenterApi
      .isOpen()
      .then((state) => {
        presentOpen.value = state.open;
      })
      .catch(() => {
        // 非 Electron 环境（浏览器调试）下无投屏能力，静默降级
      });
  });

  onBeforeUnmount(() => {
    disposeState?.();
    // 离开页面即关闭第二屏，避免残留无数据的投屏窗口
    void presenterApi.close();
  });

  return { presentOpen, open, close, toggle };
}
