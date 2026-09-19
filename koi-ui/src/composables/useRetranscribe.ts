import { onScopeDispose, ref } from 'vue';
import { meetingApi } from '../services/meetingApi';

/** 重新转写进度条状态 */
export interface RetranscribeState {
  visible: boolean;
  status: 'pending' | 'running' | 'completed' | 'failed' | '';
  progress: number;
  step: string;
  error: string;
}

const POLL_INTERVAL_MS = 1500;

export interface UseRetranscribeOptions {
  /** 目标会议 id（getter 形式，跟随路由变化） */
  meetingId: () => number;
  /** 任务提交前的视图清理（销毁播放器、清空旧转写等） */
  onBegin?: () => void;
  /** 转写完成后的视图刷新（重新拉取会议与转写内容） */
  onCompleted?: () => Promise<void> | void;
  /** 失败提示回调 */
  onError?: (message: string) => void;
}

/**
 * 「重新转写」流程封装：提交任务 → 轮询进度 → 完成/失败收尾。
 * 组件作用域销毁时自动停止轮询。
 */
export function useRetranscribe(options: UseRetranscribeOptions) {
  const retranscribing = ref(false);
  const state = ref<RetranscribeState>({ visible: false, status: '', progress: 0, step: '', error: '' });
  let timer: ReturnType<typeof setInterval> | null = null;

  function stopPolling() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function pollProgress() {
    stopPolling();
    timer = setInterval(async () => {
      try {
        const p = await meetingApi.getTranscriptionProgress(options.meetingId());
        state.value = {
          visible: true,
          status: p.status,
          progress: p.progress || 0,
          step: p.current_step || '',
          error: p.error_message || '',
        };
        if (p.status === 'completed') {
          stopPolling();
          retranscribing.value = false;
          try {
            await options.onCompleted?.();
          } finally {
            state.value = { ...state.value, visible: false };
          }
        } else if (p.status === 'failed') {
          stopPolling();
          retranscribing.value = false;
          options.onError?.('重新转写失败：' + (p.error_message || '未知错误'));
        }
      } catch {
        // 网络抖动时继续轮询，由完成/失败状态决定结束
      }
    }, POLL_INTERVAL_MS);
  }

  async function start() {
    if (retranscribing.value) return;
    options.onBegin?.();
    stopPolling();
    state.value = { visible: true, status: 'pending', progress: 0, step: '正在提交转写任务…', error: '' };
    retranscribing.value = true;
    try {
      await meetingApi.retranscribeMeeting(options.meetingId());
      pollProgress();
    } catch (e) {
      retranscribing.value = false;
      state.value = { ...state.value, status: 'failed', error: (e as { message?: string })?.message || '未知错误' };
      options.onError?.('触发重新转写失败：' + ((e as { message?: string })?.message || '未知错误'));
    }
  }

  onScopeDispose(stopPolling);

  return { state, retranscribing, start, stopPolling };
}
