<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import {
  ArrowDownOutlined,
  ClockCircleOutlined,
  CloseOutlined,
  FullscreenExitOutlined,
  FullscreenOutlined,
} from '@antdv-next/icons';
import presenterApi from '../../services/presenter';
import { watchWindowControlsInsets, windowApi } from '../../services/windowControls';
import socketioService, { SOCKET_URL, type TranscriptPayload } from '../../services/socketio';
import { meetingApi } from '../../services/meetingApi';
import { useSpeakerStore, type Speaker } from '../../store/speaker';
import { resolveTranscriptSpeaker } from '../../utils/speakerResolve';

/**
 * 第二屏投屏页：在独立 Electron 窗口中展示实时转写内容。
 *
 * 与实时转写页一致，本页自行建立 Socket.IO 连接接收数据（不经主进程 IPC 转发）：
 * - 以 viewer 角色 join-meeting，加入会议只读观众频道，只收不发（不上行音频）
 * - 进入时先拉取已落库的历史转写补齐内容，再叠加后续实时增量
 * 主进程只负责开窗 / 关窗 / 全屏等窗口管理（见 services/presenter.ts）。
 */

const route = useRoute();
const speakerStore = useSpeakerStore();

/** 展示用转写分段 */
interface Segment {
  id: number;
  speakerName: string;
  text: string;
  time: string;
  /** 去重键：段落相对时间 + 文本（历史回填与实时增量可能重叠） */
  key: string;
}

/** 历史回填单页条数 */
const PAGE_SIZE = 200;
/** 转写服务地址（展示在空状态，便于定位连不上服务的问题） */
const socketUrl = SOCKET_URL;
/** 收到数据后判定为「转写中」的时长（毫秒） */
const ACTIVE_WINDOW_MS = 6000;

const meetingId = computed(() => Number(route.query.meetingId) || 0);
const meetingName = ref((route.query.name as string) || '实时转写');
const participantCount = ref(Number(route.query.participants) || 0);
const recordMode = ref<'mic' | 'system'>((route.query.recordMode as 'mic' | 'system') || 'mic');
const meetingTimeLabel = ref((route.query.meetingTime as string) || '');
/** 打开投屏时主窗口已进行的时长（秒），之后本地递增 */
const elapsed = ref(Number(route.query.elapsed) || 0);

const segments = ref<Segment[]>([]);
const interimText = ref('');
const interimSpeakerName = ref('');

/** Socket 已连接 */
const connected = ref(false);
/** 连接失败信息（展示在空状态区，并提供重试入口） */
const connectError = ref('');
/** 后端已确认加入会议观众频道 */
const joined = ref(false);
/** 最近一次收到转写内容的时刻 */
const lastActivityAt = ref(0);
/** 每秒刷新一次的时钟，用于推导「转写中」状态 */
const nowTick = ref(Date.now());

const fullScreen = ref(false);
const isMac = ref(false);

/** 会议配置的说话人 id：先取打开窗口时携带的 query，接口返回后用接口结果覆盖 */
let configuredSpeakerIds: number[] =
  (route.query.speakers as string)?.split(',').filter(Boolean).map(Number) ?? [];
/** 会议配置的说话人列表（用于把后端下发的 speaker 映射为本地名称） */
const configuredSpeakers = ref<Speaker[]>([]);

/** 依据说话人 id 解析出本地说话人对象（需 speakerStore 已加载） */
function syncConfiguredSpeakers() {
  configuredSpeakers.value = configuredSpeakerIds
    .map((id) => speakerStore.getById(id))
    .filter((s): s is Speaker => !!s);
}

let segId = 0;
let timer: number | undefined;
const segmentKeys = new Set<string>();

/* ------------------------- 展示派生状态 ------------------------- */

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

const showInterim = computed(() => !!interimText.value.trim());

/** 最近是否持续收到转写内容 */
const liveActive = computed(
  () => connected.value && joined.value && nowTick.value - lastActivityAt.value < ACTIVE_WINDOW_MS,
);

const statusTag = computed(() => {
  if (!connected.value) return { color: 'warning', text: '连接中' };
  if (!joined.value) return { color: 'processing', text: '准备中' };
  if (liveActive.value) return { color: 'success', text: '转写中' };
  return { color: 'default', text: '等待内容' };
});

const elapsedText = computed(() => {
  const total = elapsed.value;
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const base = `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  return hours > 0 ? `${String(hours).padStart(2, '0')}:${base}` : base;
});

/** 会议属性（人数 / 录音方式 / 会议时间）汇总为时长芯片的悬停提示，不占顶部空间 */
const metaTip = computed(() => {
  const parts = [`已进行 ${elapsedText.value}`];
  if (participantCount.value > 0) parts.push(`${participantCount.value} 人参会`);
  parts.push(recordMode.value === 'mic' ? '麦克风录音' : '系统内录');
  if (meetingTimeLabel.value) parts.push(meetingTimeLabel.value);
  return parts.join(' · ');
});

/* ------------------------- 分段写入 ------------------------- */

function pushSegment(segment: Omit<Segment, 'id'>) {
  if (segmentKeys.has(segment.key)) return;
  segmentKeys.add(segment.key);
  segments.value.push({ ...segment, id: segId++ });
}

/**
 * 说话人解析：与实时转写页共用同一套规则（utils/speakerResolve.ts），
 * 避免两处对「后端已显式下发 speaker 对象」等情况处理不一致。
 */
function resolveSpeaker(payload: TranscriptPayload): { id: number; name: string } {
  return resolveTranscriptSpeaker(payload, configuredSpeakers.value, (id) => speakerStore.getById(id));
}

/* ------------------------- Socket.IO 下行 ------------------------- */

function handleTranscript(payload: TranscriptPayload) {
  const text = (payload?.text ?? '').trim();
  if (!text) return;

  lastActivityAt.value = Date.now();
  const isFinal = payload.isFinal ?? payload.is_final ?? false;
  const speaker = resolveSpeaker(payload);

  if (isFinal) {
    const startMs: number = payload.startMs ?? payload.start_ms ?? 0;
    pushSegment({
      speakerName: speaker.name,
      text,
      time: formatTimestamp(startMs),
      key: `${startMs}|${text}`,
    });
    interimText.value = '';
    interimSpeakerName.value = '';
    // 读者上滑回看时只累计新内容条数，不把视图拽回底部
    if (autoFollow.value) scrollToBottom();
    else pendingCount.value += 1;
  } else {
    interimText.value = text;
    // 中间结果不带说话人信息，仅在显式下发时更新，避免覆盖已识别的名称
    if (payload.speaker != null || payload.speakerName || payload.speakerId) {
      interimSpeakerName.value = speaker.name;
    } else if (!interimSpeakerName.value) {
      interimSpeakerName.value = '识别中…';
    }
    if (autoFollow.value) scrollToBottom();
  }
}

/** 加入会议观众频道：只订阅转写结果，不上行音频 */
function joinAsViewer() {
  if (!meetingId.value) {
    console.warn('[present] 缺少 meetingId，无法订阅会议转写频道');
    return;
  }
  if (!socketioService.isConnected()) {
    pendingJoin = true;
    return;
  }
  pendingJoin = false;
  socketioService.emit('join-meeting', {
    meeting_id: meetingId.value,
    role: 'viewer',
  });
}

/** connect 事件可能在历史回填完成前到达，用该标记延迟加入 */
let pendingJoin = false;

function setupSocket() {
  socketioService.connect();
  connected.value = socketioService.isConnected();

  socketioService.on('connect', () => {
    connected.value = true;
    connectError.value = '';
    if (pendingJoin) joinAsViewer();
  });
  socketioService.on('disconnect', () => {
    connected.value = false;
    joined.value = false;
  });
  socketioService.on('connect_error', (error) => {
    connected.value = false;
    connectError.value = `无法连接转写服务：${(error as Error)?.message || '网络异常'}`;
    console.error('转写服务连接失败:', error);
  });
  socketioService.on('join-meeting-response', (response) => {
    joined.value = true;
    const name = (response as { meetingName?: string } | undefined)?.meetingName;
    if (name) meetingName.value = name;
  });
  socketioService.on('transcript', handleTranscript);
}

/** 手动重连：丢弃旧连接（会一并清理监听）后重新建立并订阅 */
function reconnect() {
  connectError.value = '';
  joined.value = false;
  // disconnect() 内部会 removeAllListeners + 置空实例，重建后需重新注册监听
  socketioService.disconnect();
  connected.value = false;
  setupSocket();
  joinAsViewer();
}

/* ------------------------- 历史转写回填 ------------------------- */

/** 加载已落库的历史转写（取最后一页），补齐打开投屏前已产生的内容 */
async function loadHistory() {
  const id = meetingId.value;
  if (!id) return;

  try {
    const probe = await meetingApi.getMeetingTranscripts(id, { page: 1, pageSize: 1 });
    const total = probe.total || 0;
    if (total <= 0) return;

    const lastPage = Math.max(1, Math.ceil(total / PAGE_SIZE));
    const page = await meetingApi.getMeetingTranscripts(id, { page: lastPage, pageSize: PAGE_SIZE });

    for (const item of page.items) {
      const text = (item.text || '').trim();
      if (!text || item.is_final === false) continue;
      if (segmentKeys.has(`${item.start_ms}|${text}`)) continue;
      pushSegment({
        speakerName: item.speaker_name || '未识别说话人',
        text,
        time: formatTimestamp(item.start_ms ?? 0),
        key: `${item.start_ms}|${text}`,
      });
    }
    scrollToBottom(true);
  } catch (err) {
    console.warn('加载历史转写失败，仅展示后续实时内容:', err);
  }
}

/** 读取会议信息：会议名 + 会话配置的说话人（供说话人名称映射） */
async function loadMeeting() {
  const id = meetingId.value;
  if (!id) return;

  try {
    const meeting = await meetingApi.getMeeting(id);
    if (meeting.name) meetingName.value = meeting.name;
    if (meeting.participants) {
      participantCount.value = meeting.participants.split(',').filter(Boolean).length;
    }
    const speakerIds = (meeting.speaker_ids || '').split(',').filter(Boolean).map(Number);
    // 接口返回为空时保留 query 传入的说话人，避免兜底归因失效
    if (speakerIds.length > 0) {
      configuredSpeakerIds = speakerIds;
      syncConfiguredSpeakers();
    }
  } catch (err) {
    console.warn('获取会议信息失败，使用打开窗口时的参数:', err);
  }
}

/* ------------------------- 滚动跟随 ------------------------- */

const listRef = ref<HTMLElement | null>(null);

/** DynamicScroller 只暴露方法，真实滚动元素需从容器查询 */
function getScrollerEl(): HTMLElement | null {
  return listRef.value?.querySelector<HTMLElement>('.vue-recycle-scroller') ?? null;
}

/** 是否处于贴底跟随（读者上滑回看时置为 false） */
const autoFollow = ref(true);
/** 暂停跟随后累计的新分段条数 */
const pendingCount = ref(0);

/** 距底部 40px 内视为仍在跟随，回到该范围即清除未读计数 */
function onListScroll(e: Event) {
  const el = e.target as HTMLElement;
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  autoFollow.value = atBottom;
  if (atBottom) pendingCount.value = 0;
}

/** 恢复跟随并跳到最新内容 */
function resumeFollow() {
  autoFollow.value = true;
  pendingCount.value = 0;
  scrollToBottom(true);
}

/**
 * 贴底滚动：跟随状态下新内容到达即滚到最新。
 * 连续几帧校正以兼容动态行高测量，避免停在离底部差一点的位置。
 * @param immediate 忽略跟随开关强制贴底（历史回填、点击「回到最新」时使用）
 */
function scrollToBottom(immediate = false) {
  if (!immediate && !autoFollow.value) return;
  nextTick(() => {
    const el = getScrollerEl();
    if (!el) return;
    const prevBehavior = el.style.scrollBehavior;
    el.style.scrollBehavior = 'auto';
    let frames = immediate ? 12 : 8;
    const stick = () => {
      el.scrollTop = el.scrollHeight;
      if (frames-- > 0) requestAnimationFrame(stick);
      else el.style.scrollBehavior = prevBehavior;
    };
    stick();
  });
}

/* ------------------------- 顶部栏操作 ------------------------- */

async function toggleFullScreen() {
  try {
    const result = await presenterApi.toggleFullScreen();
    fullScreen.value = result.fullScreen;
  } catch (err) {
    console.error('切换全屏失败:', err);
  }
}

async function closeWindow() {
  try {
    await presenterApi.close();
  } catch (err) {
    console.error('关闭投屏窗口失败:', err);
  }
}

/* ------------------------- 生命周期 ------------------------- */

let disposeInsets: (() => void) | undefined;

function tick() {
  nowTick.value = Date.now();
  elapsed.value += 1;
}

/** 执行初始化步骤，单个失败不阻断后续：转写内容的接收不依赖这些接口 */
async function safeRun(label: string, task: () => Promise<unknown>) {
  try {
    await task();
  } catch (err) {
    console.warn(`投屏初始化 - ${label} 失败:`, err);
  }
}

onMounted(async () => {
  // 先建立 Socket 连接：不依赖任何 UI / 接口初始化，
  // 避免某个初始化步骤抛错导致一直停留在「正在连接转写服务…」
  setupSocket();

  try {
    // 隐藏了全局标题栏，这里自行预留原生窗口控件安全区（Windows / Linux 的 WCO）
    disposeInsets = watchWindowControlsInsets();
  } catch (err) {
    console.warn('初始化窗口控件安全区失败:', err);
  }
  timer = window.setInterval(tick, 1000);

  try {
    isMac.value = (await windowApi.getPlatform()) === 'darwin';
  } catch {
    // 纯浏览器环境（无 Electron IPC）下按非 macOS 布局渲染
  }

  // 加载说话人库，供后端下发的 speaker 映射为本地名称
  await safeRun('加载说话人列表', () => speakerStore.load({ pageSize: 100 }));
  // 说话人库就绪后再解析一次候选说话人（query 先兜底，接口返回后会再覆盖）
  syncConfiguredSpeakers();
  await safeRun('获取会议信息', loadMeeting);
  // 先补齐历史内容，再订阅实时增量，避免回填内容插到增量之后
  await safeRun('加载历史转写', loadHistory);
  joinAsViewer();
});

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
  disposeInsets?.();
  socketioService.off('transcript', handleTranscript);
  socketioService.disconnect();
});
</script>

<template>
  <div class="present-screen" :class="{ 'is-mac': isMac }">
    <!-- 顶部栏：整条作为窗口拖拽区，按钮排除拖拽 -->
    <header class="present-bar">
      <div class="bar-main">
        <span class="live-dot" :class="{ paused: !liveActive }"></span>
        <h1 class="meeting-name">{{ meetingName }}</h1>
        <!-- 状态标签只在未就绪时出现：正常转写中由左侧活动点表达，保持投屏画面干净 -->
        <a-tag v-if="!connected || !joined" :color="statusTag.color">{{ statusTag.text }}</a-tag>
      </div>
      <div class="bar-side">
        <!-- 时长是投屏唯一需要的实时指标；会议属性收进悬停提示 -->
        <a-tooltip :title="metaTip">
          <span class="elapsed">
            <ClockCircleOutlined /> {{ elapsedText }}
          </span>
        </a-tooltip>
        <span class="bar-divider" aria-hidden="true"></span>
        <a-tooltip :title="fullScreen ? '退出全屏' : '全屏'">
          <a-button
            class="bar-btn"
            size="small"
            type="text"
            :aria-label="fullScreen ? '退出全屏' : '全屏'"
            @click="toggleFullScreen"
          >
            <template #icon>
              <component :is="fullScreen ? FullscreenExitOutlined : FullscreenOutlined" />
            </template>
          </a-button>
        </a-tooltip>
        <a-tooltip title="关闭投屏窗口">
          <a-button
            class="bar-btn"
            size="small"
            type="text"
            aria-label="关闭投屏窗口"
            @click="closeWindow"
          >
            <template #icon><CloseOutlined /></template>
          </a-button>
        </a-tooltip>
      </div>
    </header>

    <main ref="listRef" class="present-list">
      <div v-if="segments.length === 0 && !showInterim" class="present-empty">
        <span class="empty-title">{{ meetingName }}</span>
        <template v-if="connectError">
          <p class="empty-error">{{ connectError }}</p>
          <a-button size="small" @click="reconnect">重新连接</a-button>
        </template>
        <p v-else>{{ connected ? '等待实时转写内容…' : '正在连接转写服务…' }}</p>
        <p class="empty-hint">转写服务：{{ socketUrl }}</p>
      </div>

      <DynamicScroller
        v-if="segments.length > 0"
        class="present-scroller"
        :items="segments"
        :min-item-size="86"
        key-field="id"
        @scroll="onListScroll"
      >
        <template #default="{ item, index, active }">
          <!-- 3.x 起行高由 ResizeObserver 自动测量，不再需要已废弃的 size-dependencies -->
          <DynamicScrollerItem :item="item" :active="active" :data-index="index">
            <article class="present-item">
              <div class="item-head">
                <span class="speaker">{{ item.speakerName }}</span>
                <span class="time">{{ item.time }}</span>
              </div>
              <p class="text">{{ item.text }}</p>
            </article>
          </DynamicScrollerItem>
        </template>
      </DynamicScroller>

      <!-- 中间结果：定稿前实时刷新 -->
      <article v-if="showInterim" class="present-item interim">
        <div class="item-head">
          <span class="speaker interim-speaker">{{ interimSpeakerName || '识别中…' }}</span>
          <span class="time">识别中…</span>
        </div>
        <p class="text interim-text">{{ interimText }}</p>
      </article>

      <!-- 上滑回看时的新内容提示，点击恢复贴底跟随 -->
      <a-button v-if="!autoFollow" class="jump-latest" shape="round" @click="resumeFollow">
        <template #icon><ArrowDownOutlined /></template>
        {{ pendingCount > 0 ? `${pendingCount} 条新内容` : '回到最新' }}
      </a-button>
    </main>
  </div>
</template>

<style scoped>
.present-screen {
  display: flex;
  flex-direction: column;
  /* 原生窗口控件安全区：macOS 预留红绿灯，Windows / Linux 预留 WCO 按钮 */
  --bar-pad-start: calc(var(--wco-left, 0px) + 20px);
  --bar-pad-end: calc(var(--wco-right, 0px) + 20px);

  height: calc(100vh - var(--titlebar-height));
  box-sizing: border-box;
  overflow: hidden;
  color: var(--color-text);
  background: radial-gradient(
      1400px 700px at 85% -10%,
      var(--color-brand-soft),
      transparent 62%
    ),
    var(--color-bg);
}

.present-screen.is-mac {
  --bar-pad-start: 88px;
}

/* 顶部栏：无边框窗口下的拖拽区 */
.present-bar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px var(--bar-pad-end) 10px var(--bar-pad-start);
  background: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
  -webkit-app-region: drag;
  app-region: drag;
  user-select: none;
  -webkit-user-select: none;
}

.present-bar :deep(button),
.present-bar .bar-btn {
  -webkit-app-region: no-drag;
  app-region: no-drag;
}

.bar-main {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.meeting-name {
  margin: 0;
  min-width: 0;
  font-size: 22px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.live-dot {
  flex: none;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--color-success);
  box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6);
  animation: present-pulse 1.4s infinite;
}

.live-dot.paused {
  background: var(--color-warning);
  animation: none;
}

@keyframes present-pulse {
  0% {
    box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6);
  }
  70% {
    box-shadow: 0 0 0 10px rgba(82, 196, 26, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(82, 196, 26, 0);
  }
}

/* 右侧：实时指标 + 窗口控制，与左侧信息区分离 */
.bar-side {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  /* 顶部栏整体是窗口拖拽区，这里必须排除：否则指针事件被系统吞掉，
     悬停提示（tooltip）无法触发，左侧会议名区域仍可拖动窗口 */
  -webkit-app-region: no-drag;
  app-region: no-drag;
}

.elapsed {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--color-text);
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.bar-divider {
  width: 1px;
  height: 18px;
  margin: 0 2px;
  background: var(--color-border);
}

/* 控制按钮平时淡出，指针移入顶部栏或键盘聚焦时显现，让投屏画面保持干净 */
.bar-btn {
  opacity: 0.4;
  transition: opacity 0.2s ease;
}

.present-bar:hover .bar-btn,
.bar-btn:focus-visible {
  opacity: 1;
}

/* 转写区：大字号，兼顾远距离观看 */
.present-list {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 12px clamp(20px, 4vw, 64px) 24px;
}

/* 回到最新：悬浮在右下角，不遮挡正文 */
.jump-latest {
  position: absolute;
  right: clamp(20px, 4vw, 64px);
  bottom: 28px;
  box-shadow: var(--shadow-sm, 0 2px 8px rgba(0, 0, 0, 0.16));
}

.present-scroller {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  scroll-behavior: smooth;
}

.present-scroller::-webkit-scrollbar {
  width: 8px;
}

.present-scroller::-webkit-scrollbar-thumb {
  background: var(--color-border-strong);
  border-radius: 4px;
}

.present-empty {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--color-text-muted);
}

.present-empty .empty-title {
  color: var(--color-text-secondary);
  font-size: 26px;
  font-weight: 600;
}

.present-empty .empty-error {
  margin: 0;
  color: var(--color-error, #ff4d4f);
  font-size: clamp(14px, 1.1vw, 18px);
}

.present-empty .empty-hint {
  margin: 0;
  color: var(--color-text-muted);
  font-size: clamp(12px, 0.9vw, 14px);
}

.present-item {
  padding: 16px 0;
  border-bottom: 1px solid var(--color-border-secondary);
}

.present-item.interim {
  flex: none;
  border-bottom: none;
}

.item-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 6px;
}

.speaker {
  color: var(--color-brand);
  font-size: clamp(15px, 1.1vw, 18px);
  font-weight: 600;
}

.speaker.interim-speaker {
  color: var(--color-warning);
}

.time {
  color: var(--color-text-muted);
  font-size: clamp(12px, 0.9vw, 15px);
  font-variant-numeric: tabular-nums;
}

.text {
  margin: 0;
  color: var(--color-text);
  font-size: clamp(19px, 1.7vw, 30px);
  line-height: 1.7;
  word-break: break-word;
}

.interim-text {
  color: var(--color-text-secondary);
  animation: present-blink 1.5s ease-in-out infinite;
}

@keyframes present-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.6;
  }
}
</style>
