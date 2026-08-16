<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { App } from 'antdv-next';
const { message, modal } = App.useApp();
import { useSpeakerStore, type Speaker } from '../../store/speaker';
import { hotWordApi } from '../../services/hotWordApi';
import type { HotWordDTO } from '../../services/hotWordApi';
import { meetingApi, type MeetingDTO } from '../../services/meetingApi';
import {
  AudioOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  StopOutlined,
  ClockCircleOutlined,
  TeamOutlined,
  TagsOutlined,
  SoundOutlined,
  ArrowLeftOutlined,
  CalendarOutlined,
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import socketioService, { type TranscriptPayload } from '../../services/socketio';
import { createMicrophoneStream, createSystemAudioStream } from '../../services/capture';
import audioProcessorCode from '@/worklets/audio-processor.js?raw';

const route = useRoute();
const router = useRouter();
const speakerStore = useSpeakerStore();

interface Segment {
  id: number;
  speakerId: number;
  speakerName: string;
  text: string;
  time: string;
}

// 会议配置（来自创建页 query）
const meetingName = ref((route.query.name as string) || '未命名会议');
const recordMode = ref<'mic' | 'system'>((route.query.recordMode as 'mic' | 'system') || 'mic');
const participantNames = ref<string[]>(
  (route.query.participants as string)?.split(',').filter(Boolean) || [],
);
const speakerIds = ref<number[]>(
  (route.query.speakers as string)?.split(',').filter(Boolean).map(Number) || [],
);
const hotWordIds = ref<number[]>(
  (route.query.hotWords as string)?.split(',').filter(Boolean).map(Number) || [],
);
const startTime = ref((route.query.startTime as string) || '');
const endTime = ref((route.query.endTime as string) || '');
const meetingId = computed(() => (route.query.meetingId as string) || '');
const meetingTimeLabel = computed(() => {
  if (!startTime.value) return '';
  const fmt = (s: string) =>
    new Date(s).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  return endTime.value ? `${fmt(startTime.value)} ~ ${fmt(endTime.value)}` : fmt(startTime.value);
});

const participants = computed(() => participantNames.value);
const speakers = computed<Speaker[]>(() =>
  speakerIds.value.map((id) => speakerStore.getById(id)).filter((s): s is Speaker => !!s),
);

// 热词库：根据选中的库 id，通过热词库接口加载库及其热词
interface SelectedLibrary {
  id: number;
  name: string;
  words: HotWordDTO[];
}
const selectedLibraries = ref<SelectedLibrary[]>([]);
const hotWordLoading = ref(false);

async function loadHotWords() {
  if (hotWordIds.value.length === 0) {
    selectedLibraries.value = [];
    return;
  }
  hotWordLoading.value = true;
  try {
    const res = await hotWordApi.listLibraries({ pageSize: 1000 });
    const chosen = res.items.filter((lib) => hotWordIds.value.includes(lib.id));
    const libs: SelectedLibrary[] = await Promise.all(
      chosen.map(async (lib) => {
        let words: HotWordDTO[] = [];
        try {
          const wres = await hotWordApi.listWords(lib.id);
          words = wres.items;
        } catch {
          words = [];
        }
        return { id: lib.id, name: lib.name, words };
      }),
    );
    selectedLibraries.value = libs;
  } catch (err) {
    console.error('加载热词库失败:', err);
  } finally {
    hotWordLoading.value = false;
  }
}

const hotWords = computed(() => selectedLibraries.value.flatMap((lib) => lib.words));

const running = ref(true);
const elapsed = ref(0); // 秒
const segments = ref<Segment[]>([]);

/** 转写会话是否已由用户主动开始 */
const started = ref(false);
/** 正在初始化：建立连接、等待模型就绪 */
const starting = ref(false);
/** 正在采集音频 */
const recording = ref(false);
/** 转写服务连接状态 */
const connected = ref(false);
/** 采集 / 权限错误提示 */
const captureError = ref('');
/** 实时输入音量（0~1），用于状态指示 */
const currentVolume = ref(0);
/** 后端下发的中间结果（未定稿），定稿后并入 segments */
const interimText = ref('');
const interimSpeakerId = ref<number>(-1);
const interimSpeakerName = ref('');

let timer: number | undefined;
let segId = 0;

function nowTime(base?: Date) {
  const d = base ?? new Date();
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
}

/** 毫秒时间戳 → 相对音频开头的偏移（支持天/小时） */
function formatTimestamp(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const remain = totalSeconds % 86400;
  const hours = Math.floor(remain / 3600);
  const minutes = Math.floor((remain % 3600) / 60);
  const seconds = remain % 60;
  const hms = [
    String(hours).padStart(2, '0'),
    String(minutes).padStart(2, '0'),
    String(seconds).padStart(2, '0'),
  ].join(':');
  return days > 0 ? `${days}天 ${hms}` : hms;
}

/* ------------------------- 音频采集 -> Socket.IO 上行 ------------------------- */

/** 转写服务要求的采样率 */
const TARGET_SAMPLE_RATE = 16000;
/** 每帧上行的 PCM 采样点数 */
const PCM_CHUNK_SIZE = 512;

let audioContext: AudioContext | null = null;
let mediaStream: MediaStream | null = null;
let sourceNode: MediaStreamAudioSourceNode | null = null;
let workletNode: AudioWorkletNode | null = null;
/** 零增益节点：保证 worklet 处于渲染图中被驱动，同时避免本机回放造成啸叫 */
let muteNode: GainNode | null = null;
/** 系统内录的释放函数（含内部占位视频轨） */
let stopSystemCapture: (() => void) | null = null;
let workletReady = false;
/** 未满一帧的 PCM 余量 */
let pcmBuffer = new Int16Array(0);
let volumeTick = 0;

/** 懒初始化 16kHz AudioContext 并注册 audio-processor 模块 */
async function ensureAudioGraph() {
  if (!audioContext || audioContext.state === 'closed') {
    audioContext = new AudioContext({ sampleRate: TARGET_SAMPLE_RATE });
    workletReady = false;
  }
  if (audioContext.state === 'suspended') {
    await audioContext.resume();
  }
  if (!workletReady) {
    const blob = new Blob([audioProcessorCode], { type: 'application/javascript' });
    const url = URL.createObjectURL(blob);
    try {
      await audioContext.audioWorklet.addModule(url);
      workletReady = true;
    } finally {
      URL.revokeObjectURL(url);
    }
  }
}

/** worklet 回传 16bit PCM：累积成固定长度分片后经 Socket.IO 上行 */
function handleWorkletMessage(event: MessageEvent) {
  const chunk = new Int16Array(event.data as ArrayBuffer);

  // 音量指示（降频更新，避免高频渲染）
  if (++volumeTick % 4 === 0) {
    let sum = 0;
    for (let i = 0; i < chunk.length; i++) {
      const v = chunk[i] / 32768;
      sum += v * v;
    }
    currentVolume.value = chunk.length ? Math.min(1, Math.sqrt(sum / chunk.length) * 5.5) : 0;
  }

  // 暂停期间不上行音频，仅保留采集链路
  if (!recording.value || !running.value) return;

  const merged = new Int16Array(pcmBuffer.length + chunk.length);
  merged.set(pcmBuffer);
  merged.set(chunk, pcmBuffer.length);
  pcmBuffer = merged;

  while (pcmBuffer.length >= PCM_CHUNK_SIZE) {
    const frame = pcmBuffer.slice(0, PCM_CHUNK_SIZE);
    pcmBuffer = pcmBuffer.slice(PCM_CHUNK_SIZE);
    try {
      socketioService.emit('with-binary', frame.buffer, 1);
    } catch (err) {
      console.error('发送音频数据失败:', err);
      captureError.value = '音频上行失败，请检查转写服务连接';
      void stopCapture(false);
      return;
    }
  }
}

/** 按会议配置的录音方式开始采集 */
async function startCapture() {
  if (recording.value) return;
  captureError.value = '';
  pcmBuffer = new Int16Array(0);

  try {
    await ensureAudioGraph();

    if (recordMode.value === 'mic') {
      mediaStream = await createMicrophoneStream();
    } else {
      const capture = await createSystemAudioStream({ silent: false });
      mediaStream = capture.stream;
      stopSystemCapture = capture.stop;
    }

    sourceNode = audioContext!.createMediaStreamSource(mediaStream);
    workletNode = new AudioWorkletNode(audioContext!, 'audio-processor');
    workletNode.port.onmessage = handleWorkletMessage;
    muteNode = audioContext!.createGain();
    muteNode.gain.value = 0;

    sourceNode.connect(workletNode);
    workletNode.connect(muteNode);
    muteNode.connect(audioContext!.destination);

    // 用户在系统层结束共享 / 拔出设备时同步收尾
    const track = mediaStream.getAudioTracks()[0];
    if (track) track.onended = () => void stopCapture();

    recording.value = true;
  } catch (err) {
    captureError.value = (err as Error)?.message || '音频采集启动失败';
    message.error(captureError.value);
    await stopCapture(false);
  }
}

/**
 * 结束采集并释放音频链路。
 * @param sendFinal 是否向后端发送结束帧（flag=0），用于触发最后一段文本定稿
 */
async function stopCapture(sendFinal = true) {
  const wasRecording = recording.value;
  recording.value = false;

  if (mediaStream) {
    mediaStream.getTracks().forEach((t) => t.stop());
    mediaStream = null;
  }
  if (stopSystemCapture) {
    stopSystemCapture();
    stopSystemCapture = null;
  }
  if (workletNode) {
    workletNode.port.onmessage = null;
    workletNode.disconnect();
    workletNode = null;
  }
  if (sourceNode) {
    sourceNode.disconnect();
    sourceNode = null;
  }
  if (muteNode) {
    muteNode.disconnect();
    muteNode = null;
  }

  pcmBuffer = new Int16Array(0);
  currentVolume.value = 0;

  if (wasRecording && sendFinal && socketioService.isConnected()) {
    socketioService.emit('with-binary', new ArrayBuffer(0), 0);
    // 等待结束帧发出，便于后端定稿最后一段文本
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
}

/** 释放 AudioContext（仅在离开页面时调用） */
function closeAudioContext() {
  if (audioContext && audioContext.state !== 'closed') {
    void audioContext.close();
  }
  audioContext = null;
  workletReady = false;
}

/* ------------------------- Socket.IO 下行转写结果 ------------------------- */

/** 将后端下发的说话人信息映射到本地说话人库 */
function resolveSpeaker(payload: TranscriptPayload): { id: number; name: string } {
  // 规范化 speaker 字段：后端新版协议下 speaker 为嵌套对象 {name, id, ...}
  const speakerObj = typeof payload.speaker === 'object' && payload.speaker !== null
    ? payload.speaker
    : null;
  const speakerName: string | undefined =
    payload.speakerName ||
    (speakerObj ? speakerObj.name : undefined) ||
    (typeof payload.speaker === 'string' ? payload.speaker : undefined);
  const speakerObjId = speakerObj?.id != null ? Number(speakerObj.id) : undefined;

  // 优先使用 speaker 对象中的 id，其次使用顶层 speakerId / speaker_id
  const rawId = speakerObjId ?? payload.speakerId ?? payload.speaker_id;
  if (rawId !== undefined && rawId !== null && rawId !== '') {
    const id = Number(rawId);
    if (!Number.isNaN(id)) {
      const matched = speakers.value.find((s) => s.id === id) ?? speakerStore.getById(id);
      if (matched) return { id: matched.id, name: matched.name };
      return { id, name: speakerName || `说话人 ${id}` };
    }
  }

  // 后端显式下发了 speaker 对象 → 后端已做声纹识别，必须尊重其结果
  // 无 id 的 speaker 对象（如 {name: "未知说话人"}）意味着未匹配到已知说话人，
  // 不应跳过此结果进入客户端的"配置仅一位→归给该人"兜底逻辑
  if (speakerObj) {
    if (speakerName && speakerName !== '未知说话人') {
      const matched = speakers.value.find((s) => s.name === speakerName);
      return matched ? { id: matched.id, name: matched.name } : { id: -1, name: speakerName };
    }
    return { id: -1, name: '未知说话人' };
  }

  if (speakerName && speakerName !== '未知说话人') {
    const matched = speakers.value.find((s) => s.name === speakerName);
    return matched ? { id: matched.id, name: matched.name } : { id: -1, name: speakerName };
  }

  // 后端未做说话人分离或识别失败：仅配置一位说话人时归属该人
  if (speakers.value.length === 1) {
    return { id: speakers.value[0].id, name: speakers.value[0].name };
  }
  return { id: -1, name: '未识别说话人' };
}

function clearInterim() {
  interimText.value = '';
  interimSpeakerId.value = -1;
  interimSpeakerName.value = '';
}

function handleTranscript(payload: TranscriptPayload) {
  const text = (payload?.text ?? '').trim();
  if (!text) return;

  const isFinal = payload.isFinal ?? payload.is_final ?? false;
  const speaker = resolveSpeaker(payload);

  if (isFinal) {
    const startMs: number = payload.startMs ?? payload.start_ms ?? 0;
    segments.value.push({
      id: segId++,
      speakerId: speaker.id,
      speakerName: speaker.name,
      text,
      time: formatTimestamp(startMs),
    });
    clearInterim();
    scrollToBottom();
  } else {
    interimText.value = text;
    // 中间结果：后端只下发 text + isFinal，不包含 speaker 信息（此时
    // 说话人识别尚未完成）。仅当后端显式提供了 speaker 时才更新说话人，
    // 避免客户端猜测覆盖正确的说话人显示。
    if (payload.speaker != null || payload.speakerName || payload.speakerId) {
      interimSpeakerId.value = speaker.id;
      interimSpeakerName.value = speaker.name;
    } else if (!interimSpeakerName.value) {
      interimSpeakerName.value = '识别中…';
    }
    scrollToBottom();
  }
}

function handleConnect() {
  connected.value = true;
  // 通知后端当前客户端所属会议，触发声纹预热、热词加载、session 绑定
  if (meetingId.value) {
    socketioService.emit('join-meeting', { meeting_id: Number(meetingId.value) });
  }
}

function handleDisconnect(reason: string) {
  connected.value = false;
  console.warn('转写服务连接断开:', reason);
  if (recording.value) {
    message.warning('转写服务连接断开，正在尝试重连');
  }
}

function handleSocketError(error: unknown) {
  connected.value = false;
  console.error('转写服务异常:', error);
}

/** 建立转写连接并注册事件 */
function setupSocket() {
  socketioService.connect();
  connected.value = socketioService.isConnected();
  socketioService.on('connect', handleConnect);
  socketioService.on('disconnect', handleDisconnect);
  socketioService.on('connect_error', handleSocketError);
  socketioService.on('error', handleSocketError);
  socketioService.on('transcript', handleTranscript);
}

/** 结束采集、断开连接（返回上一页 / 结束会议 / 卸载时统一收尾） */
async function teardown(sendFinal = true) {
  stopTimer();
  await stopCapture(sendFinal);
  socketioService.disconnect();
  connected.value = false;
  closeAudioContext();
}

// DynamicScroller 通过 expose 只暴露方法，取不到 $el，因此用容器 ref 查询
// 真实的滚动元素（.vue-recycle-scroller 根节点，即真正可滚动的容器）。
const transcriptPanelRef = ref<HTMLElement | null>(null);
const autoScroll = ref(true);

function getScrollerEl(): HTMLElement | null {
  const panel = transcriptPanelRef.value;
  if (!panel) return null;
  return panel.querySelector<HTMLElement>('.vue-recycle-scroller');
}

function onTranscriptScroll(e: Event) {
  const el = e.target as HTMLElement;
  autoScroll.value = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}

/**
 * 页面重新获得焦点（或切回标签页）时，恢复“贴底跟随”并滚动到最新转写。
 * 实时会议页面被切走再回来，用户通常想看最新内容，因此重置 autoScroll 为 true。
 */
function onWindowFocus() {
  autoScroll.value = true;
  scrollToBottom();
}

/**
 * 将虚拟滚动条贴到底部（最新转写处）。
 * 直接操作真实滚动元素，不依赖 DynamicScroller.scrollToBottom（其基于
 * “全部项测量完成”的循环在平滑滚动下会提前终止，导致停在离底部差一点的位置）。
 * 跟随期间临时关闭平滑滚动以打断与 scroll 事件的锚点漂移，并用连续几帧校正
 * 兼容动态行高测量；一旦用户手动上滑（autoScroll=false）即停止跟随。
 */
function scrollToBottom() {
  nextTick(() => {
    if (!autoScroll.value) return;
    const el = getScrollerEl();
    if (!el) return;
    const prevBehavior = el.style.scrollBehavior;
    el.style.scrollBehavior = 'auto';
    let frames = 0;
    const stick = () => {
      el.scrollTop = el.scrollHeight;
      if (autoScroll.value && frames++ < 8) {
        requestAnimationFrame(stick);
      } else {
        el.style.scrollBehavior = prevBehavior;
      }
    };
    stick();
  });
}

function tick() {
  if (!running.value) return;
  elapsed.value += 1;
}

function startTimer() {
  stopTimer();
  timer = window.setInterval(tick, 1000);
}
function stopTimer() {
  if (timer) {
    window.clearInterval(timer);
    timer = undefined;
  }
}

function togglePause() {
  running.value = !running.value;
  if (running.value) {
    // 恢复上行前清掉暂停期间的残留音频
    pcmBuffer = new Int16Array(0);
    void audioContext?.resume();
  } else {
    clearInterim();
  }
  message.info(running.value ? '已继续转写' : '已暂停转写');
}

/**
 * 用户主动开始实时转写：标记会议进行中、建立转写连接、按配置开始采集上行。
 * 页面加载后不会自动调用，需用户点击「开始转写」按钮触发。
 */
async function startTranscription() {
  if (started.value || starting.value) return;
  starting.value = true;
  try {
    started.value = true;
    running.value = true;
    startTimer();
    markMeetingOngoing();
    setupSocket();
    await startCapture();
  } catch (err) {
    captureError.value = (err as Error)?.message || '启动转写失败';
    message.error(captureError.value);
    started.value = false;
    running.value = false;
    stopTimer();
  } finally {
    starting.value = false;
  }
}

/**
 * 离开前收尾：自动结束会议；若全程没有任何转写内容则删除该会议。
 * 所有后端接口都需等待成功响应后才允许执行导航。
 * @param leave 实际执行导航的回调（确认且接口成功后才调用）
 */
async function finalizeAndLeave(leave: () => void) {
  running.value = false;
  // 先收尾音频链路，并触发后端定稿最后一段文本
  await teardown();

  const id = meetingId.value ? Number(meetingId.value) : 0;
  if (!id) {
    leave();
    return;
  }

  const hasTranscript = segments.value.length > 0;
  try {
    if (hasTranscript) {
      // 有转写内容：仅标记结束，保留会议与纪要
      await meetingApi.finishMeeting(id);
    } else {
      // 无任何转写内容：结束并删除该会议
      await meetingApi.finishMeeting(id);
      await meetingApi.deleteMeeting(id);
    }
    leave();
  } catch (err) {
    message.warning((err as Error)?.message || '结束会议失败，请重试');
  }
}

/** 点击「返回」：先弹确认框，确认后自动结束会议，等待后端成功再返回创建页 */
function backToCreate() {
    modal.confirm({
    title: '返回',
    content: '返回将结束本次实时会议，确认继续？',
    okText: '确认返回',
    cancelText: '取消',
    onOk: async () => {
      await finalizeAndLeave(() => router.push({ name: 'liveCreate' }));
    },
  });
}

function stopMeeting() {
    modal.confirm({
    title: '结束会议',
    content: '确认结束本次实时会议？结束后将停止音频录制并跳转首页。',
    okText: '结束',
    cancelText: '取消',
    onOk: async () => {
      // 立即结束音频录制（停止麦克风 / 系统音频采集），避免确认后等待后端响应期间仍在上行音频
      await stopCapture(true);
      await finalizeAndLeave(() => router.push({ name: 'home' }));
    },
  });
}

// 转写文本 HTML 转义（不再做热词高亮）
function escapeHtml(text: string) {
  const map: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  };
  return text.replace(/[&<>'"]/g, (c) => map[c]);
}

function highlight(text: string) {
  return escapeHtml(text);
}

/* ------------------------- 顶部按钮 -> 侧边抽屉 ------------------------- */
type DrawerKey = 'participants' | 'speakers' | 'hotWords';

const drawerOpen = ref(false);
const drawerKey = ref<DrawerKey>('participants');

const drawerTitleMap: Record<DrawerKey, string> = {
  participants: '参会人员',
  speakers: '说话人',
  hotWords: '热词库',
};
const drawerTitle = computed(() => drawerTitleMap[drawerKey.value]);

function openDrawer(key: DrawerKey) {
  drawerKey.value = key;
  drawerOpen.value = true;
}

const filteredParticipants = computed(() => participants.value);

const filteredSpeakers = computed(() => speakers.value);

// 按热词库分组热词
const hotWordGroups = computed(() =>
  selectedLibraries.value.map((lib) => ({
    name: lib.name,
    words: lib.words,
  })),
);

const visibleSegments = computed(() => segments.value);

/** 中间结果是否需要展示 */
const showInterim = computed(() => !!interimText.value);

/** 顶部转写状态标签 */
const statusTag = computed(() => {
  if (captureError.value) return { color: 'error', text: '采集异常' };
  if (starting.value) return { color: 'processing', text: '准备中' };
  if (!started.value) return { color: 'default', text: '未开始' };
  if (!connected.value) return { color: 'warning', text: '连接中' };
  if (!recording.value) return { color: 'default', text: '未采集' };
  if (!running.value) return { color: 'warning', text: '已暂停' };
  return { color: 'success', text: '转写中' };
});

const elapsedText = computed(() => {
  const m = Math.floor(elapsed.value / 60);
  const s = elapsed.value % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
});

/** 从会议详情接口返回值填充页面状态。 */
function applyMeetingData(meeting: MeetingDTO) {
  meetingName.value = meeting.name || '未命名会议';
  participantNames.value = meeting.participants?.split(',').filter(Boolean) || [];
  speakerIds.value = meeting.speaker_ids?.split(',').filter(Boolean).map(Number) || [];
  hotWordIds.value = meeting.hot_word_library_ids?.split(',').filter(Boolean).map(Number) || [];
  startTime.value = meeting.start_time || '';
  endTime.value = meeting.end_time || '';
}

/** 通过 meetingId 从后端获取会议详细信息。 */
async function loadMeetingDetail() {
  if (!meetingId.value) return;
  try {
    const meeting = await meetingApi.getMeeting(Number(meetingId.value));
    applyMeetingData(meeting);
  } catch (err) {
    console.error('获取会议详情失败，使用 query 参数兜底:', err);
  }
}

// 切换会议时重置（不自动开始转写，等待用户点击「开始转写」）
watch(
  () => route.query,
  async (q) => {
    // 先结束上一场会议的采集，避免音频链路串场
    await teardown(false);
    meetingName.value = (q.name as string) || '未命名会议';
    recordMode.value = (q.recordMode as 'mic' | 'system') || 'mic';
    participantNames.value =
      (q.participants as string)?.split(',').filter(Boolean) || [];
    speakerIds.value =
      (q.speakers as string)?.split(',').filter(Boolean).map(Number) || [];
    hotWordIds.value =
      (q.hotWords as string)?.split(',').filter(Boolean).map(Number) || [];
    startTime.value = (q.startTime as string) || '';
    endTime.value = (q.endTime as string) || '';
    segments.value = [];
    clearInterim();
    elapsed.value = 0;
    segId = 0;
    started.value = false;
    running.value = true;
    loadHotWords();
  },
);

onMounted(async () => {
  // 加载说话人列表，供 getById 检索转写参与者
  speakerStore.load();
  // 优先从后端 API 获取会议详情
  await loadMeetingDetail();
  // 加载所选热词库及其热词
  loadHotWords();
  // 注意：不在此处建立连接 / 开始采集，等待用户点击「开始转写」
  // 页面重新获得焦点 / 切回标签页时，滚动到最新转写
  window.addEventListener('focus', onWindowFocus);
  document.addEventListener('visibilitychange', onVisibilityChange);
});

/** 标签页切回前台时也视为“重新获得焦点”，滚动到最新转写。 */
function onVisibilityChange() {
  if (document.visibilityState === 'visible') onWindowFocus();
}

/** 进入转写页即标记会议为进行中（实时会议）。 */
function markMeetingOngoing() {
  if (!meetingId.value) return;
  meetingApi
    .startMeeting(Number(meetingId.value))
    .catch((err) => message.warning((err as Error)?.message || '标记会议进行中失败'));
}
onBeforeUnmount(() => {
  window.removeEventListener('focus', onWindowFocus);
  document.removeEventListener('visibilitychange', onVisibilityChange);
  void teardown();
});
</script>

<template>
  <div class="live-transcribe">
    <div class="top-bar">
      <div class="meeting-info">
        <a-button type="text" class="back-btn" @click="backToCreate">
          <template #icon><ArrowLeftOutlined /></template>
          返回
        </a-button>
        <div class="info-main">
          <h2>{{ meetingName }}</h2>
          <div class="info-meta">
            <span class="meta-item">
              <ClockCircleOutlined /> {{ elapsedText }}
            </span>
            <span class="meta-item">
              <TeamOutlined /> {{ participants.length }} 人参会
            </span>
            <span class="meta-item">
              <component :is="recordMode === 'mic' ? AudioOutlined : SoundOutlined" />
              {{ recordMode === 'mic' ? '麦克风录音' : '系统内录' }}
            </span>
            <span v-if="meetingTimeLabel" class="meta-item">
              <CalendarOutlined /> {{ meetingTimeLabel }}
            </span>
            <span class="meta-item volume-meta" :title="`输入音量 ${Math.round(currentVolume * 100)}%`">
              <span class="volume-meter">
                <span class="volume-meter-fill" :style="{ width: `${Math.round(currentVolume * 100)}%` }"></span>
              </span>
            </span>
          </div>
        </div>
      </div>
      <div class="top-actions">
        <a-button @click="openDrawer('participants')">
          <template #icon><TeamOutlined /></template>
          参会人员
        </a-button>
        <a-button @click="openDrawer('speakers')">
          <template #icon><SoundOutlined /></template>
          说话人
        </a-button>
        <a-button @click="openDrawer('hotWords')">
          <template #icon><TagsOutlined /></template>
          热词库
        </a-button>
        <a-button v-if="!started || starting" type="primary" :loading="starting" @click="startTranscription">
          <template #icon><PlayCircleOutlined /></template>
          {{ starting ? '正在准备…' : '开始转写' }}
        </a-button>
        <template v-if="started && !starting">
          <a-button :disabled="!recording" @click="togglePause">
            <template #icon>
              <component :is="running ? PauseCircleOutlined : PlayCircleOutlined" />
            </template>
            {{ running ? '暂停' : '继续' }}
          </a-button>
          <a-button danger @click="stopMeeting"><StopOutlined />结束</a-button>
        </template>
      </div>
    </div>

    <a-card class="transcript-card" variant="borderless">
      <template #title>
        <span class="live-title">
          <span class="live-dot" :class="{ paused: !started || !running || !recording }"></span>
          实时转写
          <a-tag :color="statusTag.color">{{ statusTag.text }}</a-tag>
        </span>
      </template>
      <a-alert
        v-if="captureError"
        class="capture-alert"
        type="error"
        show-icon
        :message="captureError"
      />
      <div ref="transcriptPanelRef" class="transcript-list">
        <div v-if="visibleSegments.length === 0 && !showInterim" class="transcript-empty">
          <AudioOutlined />
          <p v-if="!started">点击「开始转写」后，转写内容将实时显示在这里</p>
          <p v-else-if="!recording">音频采集未开启，点击「重新采集」后继续转写</p>
          <p v-else>正在聆听… 转写内容将实时显示在这里</p>
        </div>
        <DynamicScroller
          v-if="visibleSegments.length > 0"
          class="transcript-scroller"
          :items="visibleSegments"
          :min-item-size="64"
          key-field="id"
          @scroll="onTranscriptScroll"
        >
          <template #default="{ item, index, active }">
            <DynamicScrollerItem
              :item="item"
              :active="active"
              :size-dependencies="[item.text, item.speakerName]"
              :data-index="index"
            >
              <div class="transcript-item">
                <div class="seg-head">
                  <a-avatar :size="24" :style="{ backgroundColor: 'var(--color-success)' }">
                    {{ item.speakerName.charAt(0) }}
                  </a-avatar>
                  <span class="seg-speaker">{{ item.speakerName }}</span>
                  <span class="seg-time">{{ item.time }}</span>
                </div>
                <div class="seg-text" v-html="highlight(item.text)"></div>
              </div>
            </DynamicScrollerItem>
          </template>
        </DynamicScroller>

        <!-- 后端下发的中间结果：定稿前实时刷新 -->
        <div v-if="showInterim" class="transcript-item interim-item">
          <div class="seg-head">
            <a-avatar :size="24" :style="{ backgroundColor: 'var(--color-warning)' }">
              {{ interimSpeakerName.charAt(0) }}
            </a-avatar>
            <span class="seg-speaker interim-speaker">{{ interimSpeakerName }}</span>
            <span class="seg-time">识别中…</span>
          </div>
          <div class="seg-text interim-text" v-html="highlight(interimText)"></div>
        </div>
      </div>
    </a-card>

    <a-drawer
      v-model:open="drawerOpen"
      :title="drawerTitle"
      :size="460"
      placement="right"
    >
      <div class="drawer-body">
        <!-- 参会人员 -->
        <template v-if="drawerKey === 'participants'">
          <div class="detail-list">
            <div v-for="name in filteredParticipants" :key="name" class="detail-item">
              <div class="detail-main">
                <div class="detail-title">
                  <span>{{ name }}</span>
                </div>
              </div>
            </div>
            <a-empty v-if="filteredParticipants.length === 0" description="暂无匹配的参会人员" />
          </div>
        </template>

        <!-- 说话人 -->
        <template v-else-if="drawerKey === 'speakers'">
          <div class="detail-list">
            <div v-for="s in filteredSpeakers" :key="s.id" class="detail-item">
              <div class="detail-main">
                <div class="detail-title">
                  <span>{{ s.name }}</span>
                </div>
              </div>
            </div>
            <a-empty v-if="filteredSpeakers.length === 0" description="暂无匹配的说话人" />
          </div>
        </template>

        <!-- 热词库 -->
        <template v-else>
          <div class="detail-list">
            <div v-for="g in hotWordGroups" :key="g.name" class="word-group">
              <div class="group-title">{{ g.name }}</div>
              <div v-for="w in g.words" :key="w.word" class="detail-item">
                <div class="detail-main">
                  <div class="detail-title">
                    <span>{{ w.word }}</span>
                    <a-tag color="gold">权重 {{ w.weight }}</a-tag>
                  </div>
                </div>
              </div>
              <a-empty v-if="g.words.length === 0" description="该热词库暂无热词" />
            </div>
            <a-empty v-if="hotWordGroups.length === 0" description="暂无启用的热词库" />
          </div>
        </template>
      </div>
    </a-drawer>
  </div>
</template>

<style scoped>
.live-transcribe {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 100vh;
  box-sizing: border-box;
  padding: 20px 24px 32px;
  max-width: 1200px;
  margin: 0 auto;
  color: var(--color-text);
  background: radial-gradient(
      1200px 600px at 80% -10%,
      var(--color-brand-soft),
      transparent 60%
    ),
    var(--color-bg);
  --radius-lg: 18px;
  --radius-md: 12px;
  --radius-sm: 8px;
  --shadow-card: var(--shadow-sm);
  --transition-base: 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}
.top-bar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 14px 18px;
  box-shadow: var(--shadow-card);
}
.meeting-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-shrink: 1;
}
.back-btn {
  color: var(--color-text-secondary);
}
.back-btn:hover {
  color: var(--color-brand-hover) !important;
}
.info-main h2 {
  margin: 0;
  color: var(--color-text);
  font-size: 18px;
  font-weight: 600;
}
.info-meta {
  display: flex;
  gap: 16px;
  margin-top: 4px;
  color: var(--color-text-muted);
  font-size: 12px;
}
.meta-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.top-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.volume-meta {
  min-width: 72px;
}
.volume-meter {
  display: inline-block;
  width: 64px;
  height: 6px;
  border-radius: 3px;
  background: var(--color-surface-2);
  overflow: hidden;
}
.volume-meter-fill {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: var(--color-success);
  transition: width 0.12s linear;
}
.capture-alert {
  margin-bottom: 12px;
}
.transcript-card :deep(.ant-card-head-title) {
  color: var(--color-text);
}
.empty-tip {
  color: var(--color-text-muted);
  font-size: 12px;
}
.detail-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
}

/* 抽屉内容 */
.drawer-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.detail-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.detail-item {
  display: flex;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border-secondary);
}
.detail-item:last-child {
  border-bottom: none;
}
.detail-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: flex-start;
}
.detail-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  color: var(--color-text);
  font-size: 14px;
  font-weight: 600;
}
.detail-sub {
  color: var(--color-text-muted);
  font-size: 12px;
  line-height: 1.6;
  word-break: break-all;
}
.dot-split {
  margin: 0 4px;
}
.detail-metric {
  width: 100%;
}
.metric-label {
  display: block;
  margin-bottom: 2px;
  color: var(--color-text-secondary);
  font-size: 12px;
}
.config-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  background: var(--color-surface-2);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm, 8px);
}
.config-label {
  color: var(--color-text);
  font-size: 14px;
  font-weight: 600;
}
.word-group + .word-group {
  margin-top: 8px;
}
.group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-top: 8px;
  color: var(--color-text-secondary);
  font-size: 12px;
  font-weight: 600;
}
.group-count {
  padding: 0 6px;
  border-radius: 8px;
  background: var(--color-surface-2);
  color: var(--color-text-muted);
  font-weight: 400;
}
.transcript-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  height: 100%;
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-card);
}
.live-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-success);
  box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6);
  animation: pulse 1.4s infinite;
}
.live-dot.paused {
  background: var(--color-warning);
  animation: none;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6); }
  70% { box-shadow: 0 0 0 8px rgba(82, 196, 26, 0); }
  100% { box-shadow: 0 0 0 0 rgba(82, 196, 26, 0); }
}
.transcript-list {
  min-height: 200px;
}
.transcript-scroller {
  height: 60vh;
  overflow-y: auto;
  padding-right: 8px;
  scroll-behavior: smooth;
}
.transcript-scroller::-webkit-scrollbar {
  width: 6px;
}
.transcript-scroller::-webkit-scrollbar-thumb {
  background: var(--color-border-strong);
  border-radius: 3px;
}
.transcript-scroller::-webkit-scrollbar-thumb:hover {
  background: var(--color-text-muted);
}
.transcript-empty {
  text-align: center;
  color: var(--color-text-muted);
  padding: 48px 0;
}
.transcript-empty :deep(> .anticon) {
  font-size: 36px;
  display: block;
  margin-bottom: 12px;
}
.transcript-item {
  padding: 12px 12px;
  margin: 0 -12px;
  border-radius: 8px;
  border-bottom: 1px solid var(--color-border-secondary);
  transition: background 0.2s ease;
}
.transcript-item:hover {
  background: var(--color-surface-2);
}
.seg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.seg-speaker {
  color: var(--color-success);
  font-weight: 600;
  font-size: 13px;
}
.seg-time {
  color: var(--color-text-muted);
  font-size: 12px;
}
.seg-text {
  color: var(--color-text);
  line-height: 1.7;
  font-size: 14px;
}
.interim-item {
  border-bottom: none;
  background: var(--color-surface-2);
}
.interim-speaker {
  color: var(--color-warning);
}
.interim-text {
  color: var(--color-text-secondary);
  animation: interim-blink 1.5s ease-in-out infinite;
}
@keyframes interim-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.65;
  }
}
</style>
