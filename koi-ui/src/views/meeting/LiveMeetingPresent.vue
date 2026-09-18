<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
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
import socketioService, {
  SOCKET_URL,
  type SpeakerRegisteredPayload,
  type TranscriptPayload,
} from '../../services/socketio';
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
  /** 语音段起始毫秒（相对音频开头），用于按时间段回填说话人归属 */
  startMs: number;
  /** 语音段结束毫秒（相对音频开头） */
  endMs: number;
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

/**
 * 第二屏窗口是无原生按钮的自绘外壳，顶部栏只提供「全屏 / 关闭」两枚按钮；
 * 全屏态由主进程同步（含系统快捷键退出全屏的情况），据此切换图标与提示。
 */
const fullScreen = ref(false);

/* ------------------------- 全屏时自动隐藏顶部栏 ------------------------- */

/** 指针贴顶多少像素内视为「唤出区」 */
const BAR_HOT_ZONE = 8;
/** 指针离开顶部栏后的收起延迟，避免擦边划过时闪烁 */
const BAR_HIDE_DELAY = 400;

/** 顶部栏是否被指针 / 键盘焦点唤出（仅全屏时生效） */
const barRevealed = ref(false);
let barHideTimer: number | undefined;

/** 全屏时顶部栏收起，让画面占满整屏；非全屏常驻，避免找不到窗口按钮 */
const barCollapsed = computed(() => fullScreen.value && !barRevealed.value);

function clearBarHideTimer() {
  if (barHideTimer !== undefined) {
    window.clearTimeout(barHideTimer);
    barHideTimer = undefined;
  }
}

/** 唤出顶部栏，并取消待执行的收起 */
function revealBar() {
  clearBarHideTimer();
  barRevealed.value = true;
}

/** 延迟收起：已排期则不重复排期，避免指针在内容区移动时无限续期 */
function scheduleHideBar() {
  if (barHideTimer !== undefined) return;
  barHideTimer = window.setTimeout(() => {
    barHideTimer = undefined;
    barRevealed.value = false;
  }, BAR_HIDE_DELAY);
}

/** 指针贴近视口顶部即唤出（收起由顶部栏自身的 mouseleave 触发，判定更精确） */
function onScreenMouseMove(e: MouseEvent) {
  if (fullScreen.value && e.clientY <= BAR_HOT_ZONE) revealBar();
}

// 切换全屏即复位：进入全屏先收起（画面干净），退出全屏恢复常驻
watch(fullScreen, () => {
  clearBarHideTimer();
  barRevealed.value = false;
});

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
    const endMs: number = payload.endMs ?? payload.end_ms ?? startMs;
    pushSegment({
      speakerName: speaker.name,
      text,
      time: formatTimestamp(startMs),
      startMs,
      endMs: Math.max(endMs, startMs),
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

/**
 * 主窗口在转写过程中框选文字动态注册了新说话人：投屏端同步
 * ① 该时间段内已展示片段的归属；② 会议说话人范围，使后续结果按新名称解析。
 */
function handleSpeakerRegistered(payload: SpeakerRegisteredPayload) {
  if (!payload?.success) return;

  const name = payload.speaker?.name;
  const id = Number(payload.speaker?.id ?? -1);
  const startMs = Number(payload.startMs ?? 0);
  const endMs = Number(payload.endMs ?? 0);
  if (!name) return;

  if (endMs > startMs) {
    segments.value = segments.value.map((s) =>
      s.endMs > startMs && s.startMs < endMs ? { ...s, speakerName: name } : s,
    );
  }

  if (id > 0 && !configuredSpeakerIds.includes(id)) {
    configuredSpeakerIds = [...configuredSpeakerIds, id];
    syncConfiguredSpeakers();
  }
  void speakerStore
    .load({ pageSize: 100 })
    .then(() => syncConfiguredSpeakers())
    .catch(() => {
      // 说话人库刷新失败不影响展示：后端下发的 name 已足以正确标注
    });
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
  socketioService.on('speaker-registered', handleSpeakerRegistered);
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
        startMs: item.start_ms ?? 0,
        endMs: item.end_ms ?? item.start_ms ?? 0,
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
    fullScreen.value = (await presenterApi.toggleFullScreen()).fullScreen;
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

let disposeFullScreen: (() => void) | undefined;

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

  // 订阅全屏态：系统快捷键退出全屏等情况也能让按钮图标保持正确
  disposeFullScreen = presenterApi.onFullScreenChange((value) => {
    fullScreen.value = value;
  });
  await safeRun('同步全屏状态', async () => {
    fullScreen.value = await presenterApi.getFullScreen();
  });
  timer = window.setInterval(tick, 1000);

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
  clearBarHideTimer();
  disposeFullScreen?.();
  socketioService.off('transcript', handleTranscript);
  socketioService.disconnect();
});
</script>

<template>
  <div class="present-screen" @mousemove="onScreenMouseMove">
    <!--
      顶部栏即窗口外壳：左侧整条为拖拽区，右侧窗口控制按钮单独排除拖拽。
      全屏时容器折叠到 0 高（指针贴顶或键盘聚焦时展开），让转写内容占满整屏。
    -->
    <div class="bar-shell" :class="{ 'is-collapsed': barCollapsed }" @focusin="revealBar">
      <header class="present-bar" @mouseleave="scheduleHideBar">
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

          <!-- 窗口控制：投屏场景只需要全屏与关闭，不展示最小化 / 最大化 -->
          <div class="win-controls" role="group" aria-label="窗口控制">
            <a-tooltip :title="fullScreen ? '退出全屏' : '全屏'">
              <button
                class="win-btn"
                type="button"
                :aria-label="fullScreen ? '退出全屏' : '进入全屏'"
                @click="toggleFullScreen"
              >
                <component :is="fullScreen ? FullscreenExitOutlined : FullscreenOutlined" />
              </button>
            </a-tooltip>
            <a-tooltip title="关闭投屏窗口">
              <button
                class="win-btn win-btn--close"
                type="button"
                aria-label="关闭投屏窗口"
                @click="closeWindow"
              >
                <CloseOutlined />
              </button>
            </a-tooltip>
          </div>
        </div>
      </header>
    </div>

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
  /* 窗口无原生按钮，顶部栏两端无需再预留系统控件安全区 */
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

/* 顶部栏容器：全屏时折叠到 0 高（grid 行高可动画），转写内容随之上移到整屏 */
.bar-shell {
  flex: none;
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.26s cubic-bezier(0.4, 0, 0.2, 1);
}

.bar-shell.is-collapsed {
  grid-template-rows: 0fr;
}

/* 顶部栏：无边框无系统按钮，整条即窗口拖拽区 */
.present-bar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  /* 折叠动画需要子项可压缩：border-box 让内边距计入行高，min-height 解除内容撑高 */
  box-sizing: border-box;
  min-height: 0;
  overflow: hidden;
  padding: 10px 20px;
  background: var(--color-surface);
  transition: opacity 0.18s ease;
  border-bottom: 1px solid var(--color-border);
  -webkit-app-region: drag;
  app-region: drag;
  user-select: none;
  -webkit-user-select: none;
}

/* 折叠过程中同步淡出，避免文字被压扁时露出 */
.bar-shell.is-collapsed .present-bar {
  opacity: 0;
}

.present-bar :deep(button),
.present-bar .win-controls {
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

/* ---- 窗口控制：一枚内嵌式按钮排，替代系统窗口按钮 ---- */
.win-controls {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border: 1px solid var(--color-border-secondary);
  border-radius: var(--radius-lg, 12px);
  background: var(--color-surface-2);
  /* 平时收敛，指针进入顶部栏或键盘聚焦时完整显现，让投屏画面保持干净 */
  opacity: 0.55;
  transition: opacity 0.2s ease;
}

.present-bar:hover .win-controls,
.win-controls:focus-within {
  opacity: 1;
}

.win-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: calc(var(--radius-md, 8px) - 2px);
  background: transparent;
  color: var(--color-text-secondary);
  font-size: 14px;
  line-height: 0;
  cursor: pointer;
  transition: background 0.16s ease, color 0.16s ease, box-shadow 0.16s ease;
}

/* 悬停用品牌浅底而非纯表面色：明暗两套主题下都能稳定辨认为「可按」 */
.win-btn:hover {
  background: var(--color-brand-soft);
  color: var(--color-brand);
}

.win-btn:active {
  background: color-mix(in srgb, var(--color-brand) 20%, transparent);
}

.win-btn:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: -1px;
}

/* 关闭是破坏性操作：悬停时用系统危险色明确区分 */
.win-btn--close:hover,
.win-btn--close:active {
  background: var(--color-error);
  color: #fff;
  box-shadow: none;
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
  /* 投屏画面不出现滚动条：滚轮 / 触控板滚动与贴底跟随仍然可用 */
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.present-scroller::-webkit-scrollbar {
  display: none;
  width: 0;
  height: 0;
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
