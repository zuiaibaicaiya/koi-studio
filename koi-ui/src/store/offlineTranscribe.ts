import { defineStore } from 'pinia';
import { ref } from 'vue';

/**
 * 离线转写：在「创建页」选择音频文件后，需把文件对象与元信息
 * 传递到「转写页」。File 无法通过路由 query 序列化，因此用内存 store 暂存。
 * 注意：刷新转写页会丢失文件，转写页需处理该情况并提示用户返回重选。
 */
export const useOfflineTranscribeStore = defineStore('offlineTranscribe', () => {
  const file = ref<File | null>(null);
  /** 创建页下发的会议创建参数（转写页用于创建会议 + join 上下文） */
  const pendingMeeting = ref<{
    name: string;
    participants: string;
    speakers: string[];
    hotWords: string[];
    meetingTime: [unknown, unknown];
  } | null>(null);

  function setFile(f: File | null) {
    file.value = f;
  }
  function setPendingMeeting(m: typeof pendingMeeting.value) {
    pendingMeeting.value = m;
  }
  function clear() {
    file.value = null;
    pendingMeeting.value = null;
  }

  return { file, pendingMeeting, setFile, setPendingMeeting, clear };
});

export default useOfflineTranscribeStore;
