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
  SoundOutlined,
  ArrowLeftOutlined,
  CalendarOutlined,
  DesktopOutlined,
  UserAddOutlined,
  DragOutlined,
  CheckCircleFilled,
  CloseOutlined,
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import socketioService, {
  type SpeakerRegisteredPayload,
  type TranscriptPayload,
} from '../../services/socketio';
import { resolveTranscriptSpeaker } from '../../utils/speakerResolve';
import { formatTimestamp } from '../../utils/time';
import { escapeHtml } from '../../utils/html';
import type { LiveTranscriptItem } from '../../types/transcript';
import { speakerColorIndex, useSpeakerPalette } from '../../composables/useSpeakerPalette';
import { useAudioCapture } from '../../composables/useAudioCapture';
import { usePresenterWindow } from '../../composables/usePresenterWindow';

const route = useRoute();
const router = useRouter();
const speakerStore = useSpeakerStore();
/** 说话人配色：色相取自当前主题色，色系/明暗切换时自动重算 */
const speakerPalette = useSpeakerPalette();

interface Segment {
  id: number;
  speakerId: number;
  speakerName: string;
  text: string;
  time: string;
  /** 语音段起始毫秒（相对音频开头），与后端转写结果同一时间基准 */
  startMs: number;
  /** 语音段结束毫秒（相对音频开头） */
  endMs: number;
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

const running = ref(true);
const elapsed = ref(0); // 秒
const segments = ref<LiveTranscriptItem[]>([]);

/** 转写会话是否已由用户主动开始 */
const started = ref(false);
/** 正在初始化：建立连接、等待模型就绪 */
const starting = ref(false);
/** 转写服务连接状态 */
const connected = ref(false);
/** 后端下发的中间结果（未定稿），定稿后并入 segments */
const interimText = ref('');
const interimSpeakerId = ref<number>(-1);
const interimSpeakerName = ref('');

let timer: number | undefined;
let segId = 0;

/* ------------------------- 第二屏投屏 ------------------------- */

const { presentOpen, toggle: togglePresent, close: closePresent } = usePresenterWindow({
  // router.resolve 生成的 href 在 hash 模式下形如 "#/live/present?...",
  // 交由主进程拼到页面地址后，需要去掉前导 '#'
  buildHref: () => {
    const href = router.resolve({
      name: 'livePresent',
      query: {
        meetingId: meetingId.value,
        name: meetingName.value,
        participants: String(participants.value.length),
        // 会议配置的说话人：投屏窗口据此做名称映射与「仅一位说话人」兜底
        speakers: speakerIds.value.join(','),
        recordMode: recordMode.value,
        meetingTime: meetingTimeLabel.value,
        elapsed: String(elapsed.value),
      },
    }).href;
    return href.startsWith('#') ? href.slice(1) : href;
  },
  onError: (msg) => message.error(msg),
});

/* ------------------------- 音频采集 -> Socket.IO 上行 ------------------------- */

const {
  recording,
  currentVolume,
  captureError,
  start: startCapture,
  stop: stopCapture,
  prepareResume,
  close: closeAudioContext,
} = useAudioCapture({
  getRecordMode: () => recordMode.value,
  // 暂停期间不上行音频，仅保留采集链路
  shouldSend: () => recording.value && running.value,
  sendFrame: (data, flag) => socketioService.emit('with-binary', data, flag),
  canSendFinal: () => socketioService.isConnected(),
  onError: (msg) => message.error(msg),
});

/* ------------------------- Socket.IO 下行转写结果 ------------------------- */

/**
 * 将后端下发的说话人信息映射到本地说话人库。
 * 规则与第二屏投屏页共用（utils/speakerResolve.ts），避免两处展示不一致。
 */
function resolveSpeaker(payload: TranscriptPayload): { id: number; name: string } {
  return resolveTranscriptSpeaker(payload, speakers.value, (id) => speakerStore.getById(id));
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
    const endMs: number = payload.endMs ?? payload.end_ms ?? startMs;
    segments.value.push({
      id: segId++,
      speakerId: speaker.id,
      speakerName: speaker.name,
      text,
      time: formatTimestamp(startMs),
      startMs,
      endMs: Math.max(endMs, startMs),
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

/* ------------------------- 框选转写文字 → 添加说话人 ------------------------- */

/** 框选结果：整段粒度（一个转写片段对应后端一次断句，音频区间最干净） */
interface SelectedRange {
  startMs: number;
  endMs: number;
  /** 选中片段的文本（拼接后作为声纹音频备注回传后端，便于回溯） */
  text: string;
  /** 选中片段条数 */
  count: number;
}

/* ---- 框选门槛：两道条件同时满足才算「生效」 ---- */

/** 条件一：按住鼠标左键的最短时长（毫秒）。持续按住可避免误框选。 */
const MIN_HOLD_MS = 2000;
/** 条件二：框选文字覆盖的最短音频时长（毫秒）。保证声纹样本足够长、可用。
 *  后端 realtime 注册的有效语音下限为 1s，2s 仍有余量。 */
const MIN_SELECT_MS = 2000;
/** 低于该时长的按压视为普通点击，不给「时长不足」提示，避免误触时被提示打扰。 */
const CLICK_MS = 800;
/** 门槛秒数（提示文案使用） */
const HOLD_SECONDS = MIN_HOLD_MS / 1000;
const GATE_SECONDS = MIN_SELECT_MS / 1000;

/** 是否正在按住左键框选 */
const holding = ref(false);
/** 拖动过程中实时命中的片段区间（预览态，尚未生效） */
const dragRange = ref<SelectedRange | null>(null);
/** 已生效的框选：保持高亮直到用户操作或手动取消 */
const selection = ref<SelectedRange | null>(null);
/** 生效瞬间的确认动效开关 */
const confirmed = ref(false);
/** 未达标而被拒绝的框选：保留区间用于播放拒绝动效 */
const rejectedRange = ref<SelectedRange | null>(null);

/**
 * 弹窗专用框选快照。
 *
 * 弹窗打开后输入框会抢走焦点，浏览器随即清空文档选区；转写过程中新文字到达
 * 还会触发列表自动滚动，进而让浮标与实时框选一并失效。因此打开弹窗时把框选
 * 结果快照下来，弹窗内的展示、校验与提交全部只依赖该快照，与后续新文字无关。
 */
const pinnedSelection = ref<SelectedRange | null>(null);
/** 浮标位置（fixed 坐标系，锚定在选区/起手行上方居中） */
const selectionPos = ref<{ x: number; y: number } | null>(null);

const speakerModalOpen = ref(false);
const registering = ref(false);
const registerError = ref('');
const speakerForm = ref({ name: '', description: '' });

/** 待回执的注册请求标识：仅处理本次请求对应的回执 */
let pendingRegisterId = '';
let registerSeq = 0;
let registerTimeout: number | undefined;

/** 手势与动效的定时器/句柄 */
let holdStartedAt = 0;
let holdTimer: number | undefined;
let confirmTimer: number | undefined;
let rejectTimer: number | undefined;
let dragAnchorRect: DOMRect | null = null;

/** 生效框选的时长（秒） */
const selectedDuration = computed(() =>
  selection.value ? (selection.value.endMs - selection.value.startMs) / 1000 : 0,
);

/** 弹窗使用的框选（优先快照，未快照时回落到已生效的框选） */
const modalRange = computed(() => pinnedSelection.value ?? selection.value);

/** 弹窗展示的框选时长（秒） */
const modalDuration = computed(() =>
  modalRange.value ? (modalRange.value.endMs - modalRange.value.startMs) / 1000 : 0,
);

/** 浮标定位：锚定选区上方居中，并限制在视口内避免溢出 */
function updateSelectionPos(rect: DOMRect) {
  const margin = 90;
  const maxX = Math.max(margin, window.innerWidth - margin);
  selectionPos.value = {
    x: Math.min(Math.max(rect.left + rect.width / 2, margin), maxX),
    y: Math.max(rect.top, 56),
  };
}

/** 从选区端点所在节点回溯到对应的转写片段下标，找不到返回 -1 */
function segmentIndexFromNode(node: Node | null): number {
  if (!node) return -1;
  const element =
    node.nodeType === Node.ELEMENT_NODE ? (node as HTMLElement) : node.parentElement;
  const item = element?.closest<HTMLElement>('.transcript-item[data-seg-id]');
  if (!item?.dataset.segId) return -1;
  const id = Number(item.dataset.segId);

  return segments.value.findIndex((s) => s.id === id);
}

/** 片段是否与框选区间相交（与后端回填历史归属使用同一判定） */
function overlaps(seg: Segment, range: SelectedRange): boolean {
  return seg.endMs > range.startMs && seg.startMs < range.endMs;
}

/**
 * 片段当前的高亮态：
 * - is-selecting：拖动中，左侧品牌色边框 + 品牌色浅底，配合顶部进度环
 * - is-selected ：已生效，左侧成功色边框 + 成功色浅底，并带一次确认脉冲
 * - is-rejected ：未达标，左侧错误色边框 + 错误色浅底，抖动一次后自动清除
 */
function segmentSelectClass(seg: Segment): string {
  if (rejectedRange.value && overlaps(seg, rejectedRange.value)) return 'is-rejected';
  if (dragRange.value && overlaps(seg, dragRange.value)) return 'is-selecting';
  if (selection.value && overlaps(seg, selection.value)) {
    return confirmed.value ? 'is-selected is-confirm' : 'is-selected';
  }
  return '';
}

/** 把浏览器文本选区换算为「转写片段区间 + 音频时间段」，无效返回 null */
function rangeFromSelection(sel: Selection | null): SelectedRange | null {
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;

  const from = segmentIndexFromNode(sel.anchorNode);
  const to = segmentIndexFromNode(sel.focusNode);
  // 端点落在非转写内容（面板空白、中间结果等）上时不认为框选有效
  if (from < 0 || to < 0) return null;

  const picked = segments.value.slice(Math.min(from, to), Math.max(from, to) + 1);
  if (picked.length === 0) return null;

  return {
    startMs: Math.min(...picked.map((s) => s.startMs)),
    endMs: Math.max(...picked.map((s) => s.endMs)),
    text: picked.map((s) => s.text).join(''),
    count: picked.length,
  };
}

/* ---- 手势：按住左键拖动 → 持续 2 秒 → 松开生效 ---- */

/** 左键按下：开始一次框选手势（只在转写文字上起手，避免误触发） */
function onSelectStart(e: PointerEvent) {
  if (e.button !== 0) return;

  const item = (e.target as HTMLElement)?.closest?.('.transcript-item[data-seg-id]');
  if (!item) return;

  clearFeedbackTimers();
  rejectedRange.value = null;
  confirmed.value = false;
  // 新手势开始即清掉上一次的框选，避免两段高亮并存
  selection.value = null;
  dragRange.value = null;

  dragAnchorRect = item.getBoundingClientRect();
  updateSelectionPos(dragAnchorRect);

  holdStartedAt = performance.now();
  holding.value = true;
  holdTimer = window.setInterval(tickHold, 50);
}

/**
 * 按住期间的心跳：按浏览器实时选区刷新预览高亮。
 *
 * 拖动过程不给任何浮层提示（保持画面干净），只做两件事：驱动文字/片段高亮，
 * 以及在能取到选区矩形时更新浮标锚点，供松开生效后展示操作入口。
 */
function tickHold() {
  if (!holding.value) return;

  const sel = window.getSelection();
  const rect =
    sel && !sel.isCollapsed && sel.rangeCount > 0
      ? sel.getRangeAt(0).getBoundingClientRect()
      : null;

  updateSelectionPos(rect ?? dragAnchorRect ?? new DOMRect());
  dragRange.value = rangeFromSelection(sel);
}

/** 抬起（commit=true）/ 取消（commit=false）框选手势 */
function endSelect(commit: boolean) {
  if (!holding.value) return;

  const held = performance.now() - holdStartedAt;
  holding.value = false;
  if (holdTimer !== undefined) {
    window.clearInterval(holdTimer);
    holdTimer = undefined;
  }

  const range = commit ? dragRange.value : null;
  dragRange.value = null;

  if (!range) {
    // 没有框到任何转写内容：普通点击静默收尾，长按则提示正确的操作方式
    if (commit && held >= CLICK_MS) {
      message.warning('请按住鼠标左键拖动，框选要归属到该说话人的转写文字');
    }
    return;
  }

  if (held < MIN_HOLD_MS) {
    rejectSelection(
      range,
      `框选需持续按住 ${HOLD_SECONDS} 秒：本次仅 ${(held / 1000).toFixed(1)} 秒，请重新框选`,
    );
    return;
  }
  if (range.endMs - range.startMs < MIN_SELECT_MS) {
    rejectSelection(
      range,
      `所选文字仅覆盖 ${((range.endMs - range.startMs) / 1000).toFixed(1)} 秒，请至少框选 ${GATE_SECONDS} 秒的转写内容`,
    );
    return;
  }

  acceptSelection(range);
}

/** 生效：保持高亮、播放一次确认脉冲，并展示操作入口 */
function acceptSelection(range: SelectedRange) {
  selection.value = range;
  confirmed.value = true;

  if (confirmTimer !== undefined) window.clearTimeout(confirmTimer);
  confirmTimer = window.setTimeout(() => {
    confirmed.value = false;
  }, 620);
}

/** 未达标：清掉文字高亮，改为错误色动效 + 说明具体原因 */
function rejectSelection(range: SelectedRange, warning: string) {
  selection.value = null;
  pinnedSelection.value = null;
  rejectedRange.value = range;
  window.getSelection()?.removeAllRanges();
  message.warning(warning);

  if (rejectTimer !== undefined) window.clearTimeout(rejectTimer);
  rejectTimer = window.setTimeout(() => {
    rejectedRange.value = null;
  }, 1200);
}

/** 手动取消框选（浮标上的「取消」按钮） */
function dismissSelection() {
  clearFeedbackTimers();
  rejectedRange.value = null;
  window.getSelection()?.removeAllRanges();
  clearSelection();
}

/** 抬起/取消绑在 window 上：指针移出面板甚至移出窗口也能正确收尾 */
function onWindowPointerUp(e: PointerEvent) {
  if (e.button !== 0) return;
  endSelect(true);
}

function onWindowPointerCancel() {
  endSelect(false);
}

function onWindowBlur() {
  endSelect(false);
}

/**
 * 滚动时按真实选区重新定位浮标。
 *
 * 已生效的框选不会因为滚动、新文字或虚拟滚动回收节点而丢失：真正的高亮由
 * 「片段自身的选中态样式」承担，浮标只在能取到选区矩形时跟随。
 */
function syncSelectionBar() {
  // 弹窗打开时输入框已抢走焦点、文档选区必然已失效：此时不做任何处理，
  // 避免新文字带来的自动滚动把正在填写的注册操作「清空」。
  if (speakerModalOpen.value) return;
  if (!selection.value) return;

  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return;
  const rect = sel.getRangeAt(0).getBoundingClientRect();
  if (rect.width === 0 && rect.height === 0) return;
  updateSelectionPos(rect);
}

function clearSelection() {
  selection.value = null;
  selectionPos.value = null;
  confirmed.value = false;
}

/** 清空一次性反馈的定时器（新手势开始或页面卸载时调用） */
function clearFeedbackTimers() {
  if (confirmTimer !== undefined) {
    window.clearTimeout(confirmTimer);
    confirmTimer = undefined;
  }
  if (rejectTimer !== undefined) {
    window.clearTimeout(rejectTimer);
    rejectTimer = undefined;
  }
}

/**
 * 打开「添加为说话人」弹窗。
 *
 * 先把当前框选快照到 pinnedSelection：弹窗内的一切展示与提交都基于快照，
 * 因此转写过程中新文字到达、列表自动滚动、输入框抢焦点都不会影响本次注册。
 */
function openSpeakerModal() {
  if (!selection.value) return;

  pinnedSelection.value = { ...selection.value };
  registerError.value = '';
  speakerModalOpen.value = true;
}

function closeSpeakerModal() {
  speakerModalOpen.value = false;
  pinnedSelection.value = null;
  registerError.value = '';
  registering.value = false;
  clearRegisterTimeout();
  pendingRegisterId = '';
  // 取消后重新对齐浮标：选中的文字若仍在则保留入口，已随新文字失效则自动收起
  syncSelectionBar();
}

function clearRegisterTimeout() {
  if (registerTimeout !== undefined) {
    window.clearTimeout(registerTimeout);
    registerTimeout = undefined;
  }
}

/**
 * 提交注册请求：把框选片段的起止时间（毫秒）与说话人名称交给后端，
 * 后端据此从会话录音中截取音频提取声纹并动态注册说话人。
 */
function submitSpeakerRegistration() {
  // 始终以弹窗快照为准：即使期间有新文字到达、选区被清空，本次注册依然成立
  const range = modalRange.value;
  const name = speakerForm.value.name.trim();

  if (!range) {
    registerError.value = '请先框选要归属到该说话人的转写文字';
    return;
  }
  if (range.endMs - range.startMs < MIN_SELECT_MS) {
    registerError.value = `所选片段过短，请至少框选 ${GATE_SECONDS} 秒的转写文字`;
    return;
  }
  if (!name) {
    registerError.value = '请填写说话人名称';
    return;
  }
  if (!socketioService.isConnected()) {
    registerError.value = '转写服务未连接，请稍后重试';
    return;
  }

  registering.value = true;
  registerError.value = '';
  pendingRegisterId = `speaker-${Date.now()}-${++registerSeq}`;

  socketioService.emit('register-speaker', {
    request_id: pendingRegisterId,
    meeting_id: meetingId.value ? Number(meetingId.value) : 0,
    start_ms: range.startMs,
    end_ms: range.endMs,
    name,
    description: speakerForm.value.description.trim(),
    text: range.text.slice(0, 500),
  });

  // 兜底：连接异常导致回执缺失时恢复按钮，避免弹窗卡在 loading
  clearRegisterTimeout();
  registerTimeout = window.setTimeout(() => {
    if (!registering.value) return;
    registering.value = false;
    registerError.value = '注册超时，请重试';
  }, 20000);
}

/** 把时间段内的已展示片段改判为指定说话人（后端已完成同样的落库修正） */
function relabelSegments(startMs: number, endMs: number, speakerId: number, speakerName: string) {
  segments.value = segments.value.map((s) =>
    s.endMs > startMs && s.startMs < endMs
      ? { ...s, speakerId, speakerName }
      : s,
  );
}

/**
 * 同步注册结果：把该时间段内的片段改判为新说话人，并把说话人并入会议说话人列表。
 * 返回展示用名称。
 */
function applyRegisteredSpeaker(
  payload: SpeakerRegisteredPayload,
  fallback: SelectedRange | null,
): string {
  const name = payload.speaker?.name || speakerForm.value.name.trim();
  const id = Number(payload.speaker?.id ?? -1);
  const startMs = Number(payload.startMs ?? fallback?.startMs ?? 0);
  const endMs = Number(payload.endMs ?? fallback?.endMs ?? 0);

  if (id > 0 && endMs > startMs) {
    relabelSegments(startMs, endMs, id, name);
    // 会议说话人列表同步新增，供「会议信息 → 说话人」与后续结果解析使用
    if (!speakerIds.value.includes(id)) speakerIds.value = [...speakerIds.value, id];
  }
  // 刷新说话人库，使后续下发的 speaker.id 能映射到本地名称
  void speakerStore.load({ pageSize: 100 });

  return name;
}

/**
 * 处理后端注册回执。
 *
 * 只有「本端发起且仍在等待中的请求」才允许收尾弹窗状态；其它回执（如其它端在同一
 * 会议内注册了说话人）只做展示同步，绝不打扰用户正在填写或正在转写的界面。
 */
function handleSpeakerRegistered(payload: SpeakerRegisteredPayload) {
  const requestId = payload?.requestId ?? '';
  const isPendingReply = !!pendingRegisterId && requestId === pendingRegisterId;

  if (isPendingReply) {
    clearRegisterTimeout();
    registering.value = false;
    pendingRegisterId = '';
  }

  if (!payload?.success) {
    // 失败只反馈给发起方，避免弹出与本次操作无关的错误
    if (!isPendingReply) return;
    registerError.value = payload?.message || '注册说话人失败，请重试';
    message.error(registerError.value);
    return;
  }

  // 快照在 closeSpeakerModal 中清空，这里先取用，保证回填区间可用
  const name = applyRegisteredSpeaker(payload, modalRange.value);

  if (!isPendingReply) {
    message.info(`已添加说话人「${name}」`);
    return;
  }

  message.success(`已添加说话人「${name}」，后续转写将自动区分该说话人`);
  closeSpeakerModal();
  window.getSelection()?.removeAllRanges();
  clearSelection();
  speakerForm.value = { name: '', description: '' };
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
  socketioService.on('speaker-registered', handleSpeakerRegistered);
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
  // 浮标锚定在浏览器选区上：滚动后重新贴合，选区被虚拟滚动回收时自动收起
  syncSelectionBar();
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
  // 正在填写「添加说话人」弹窗、或正按住左键框选时都不滚动列表：
  // 前者避免打扰填表，后者避免新文字把正在拖动中的选区连同页面一起拽走。
  // 两者结束后，下一条转写会自然恢复贴底跟随。
  if (speakerModalOpen.value || holding.value) return;

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
    prepareResume();
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

/* ------------------------- 会议信息抽屉 ------------------------- */
type DrawerKey = 'participants' | 'speakers' | 'hotWords';

const drawerOpen = ref(false);
const drawerKey = ref<DrawerKey>('participants');

/** 打开会议信息抽屉；不传 key 时停留在上次查看的分组 */
function openDrawer(key: DrawerKey = drawerKey.value) {
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
    dismissSelection();
    closeSpeakerModal();
    elapsed.value = 0;
    segId = 0;
    started.value = false;
    running.value = true;
    loadHotWords();
    // 会议已切换：第二屏订阅的是上一场会议的观众频道，直接关闭避免展示错乱
    await closePresent();
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
  // 框选手势的收尾统一挂在 window 上：指针移出面板 / 移出窗口 / 窗口失焦都能正确结束
  window.addEventListener('pointerup', onWindowPointerUp);
  window.addEventListener('pointercancel', onWindowPointerCancel);
  window.addEventListener('blur', onWindowBlur);
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
  window.removeEventListener('pointerup', onWindowPointerUp);
  window.removeEventListener('pointercancel', onWindowPointerCancel);
  window.removeEventListener('blur', onWindowBlur);
  clearFeedbackTimers();
  if (holdTimer !== undefined) {
    window.clearInterval(holdTimer);
    holdTimer = undefined;
  }
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
          <!-- 只保留实时指标：会议属性（时间、人数等）收进「会议信息」抽屉 -->
          <div class="info-meta">
            <span class="elapsed-chip" title="已进行时长">
              <ClockCircleOutlined /> {{ elapsedText }}
            </span>
            <span class="meta-item" :title="`输入音量 ${Math.round(currentVolume * 100)}%`">
              <component :is="recordMode === 'mic' ? AudioOutlined : SoundOutlined" />
              <span class="record-label">{{ recordMode === 'mic' ? '麦克风录音' : '系统内录' }}</span>
              <span class="volume-meter">
                <span class="volume-meter-fill" :style="{ width: `${Math.round(currentVolume * 100)}%` }"></span>
              </span>
            </span>
          </div>
        </div>
      </div>
      <div class="top-actions">
        <a-button @click="openDrawer()">
          <template #icon><TeamOutlined /></template>
          会议信息
        </a-button>
        <a-tooltip :title="presentOpen ? '关闭第二屏投屏' : '投屏到第二屏'">
          <a-button
            class="icon-btn"
            :type="presentOpen ? 'primary' : 'default'"
            :aria-label="presentOpen ? '关闭第二屏投屏' : '投屏到第二屏'"
            @click="togglePresent"
          >
            <template #icon><DesktopOutlined /></template>
          </a-button>
        </a-tooltip>
        <span class="actions-divider" aria-hidden="true"></span>
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
      <!--
        交互提示：始终占位、仅淡出（不卸载），否则框选起手/结束时这行会让列表整体上下跳动。
      -->
      <div
        v-if="visibleSegments.length > 0"
        class="select-hint"
        :class="{ 'is-muted': holding || !!selection }"
      >
        <DragOutlined />
        按住鼠标左键拖动框选文字，持续 {{ HOLD_SECONDS }} 秒后松开即可添加说话人
      </div>

      <!--
        框选面板：pointerdown 起手，pointerup / pointercancel / blur 在 window 上收尾。
        dragstart.prevent 阻止浏览器把已选中的文字拖走（长按数秒期间很容易触发）。
      -->
      <div
        ref="transcriptPanelRef"
        class="transcript-list"
        @pointerdown="onSelectStart"
        @dragstart.prevent
      >
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
              <div
                class="transcript-item"
                :class="segmentSelectClass(item)"
                :data-seg-id="item.id"
              >
                <div class="seg-head">
                  <a-avatar :size="24" :style="{ backgroundColor: speakerPalette[speakerColorIndex(item.speakerName)] }">
                    {{ item.speakerName.charAt(0) }}
                  </a-avatar>
                  <span
                    class="seg-speaker"
                    :style="{ color: speakerPalette[speakerColorIndex(item.speakerName)] }"
                  >{{ item.speakerName }}</span>
                  <span class="seg-time">{{ item.time }}</span>
                </div>
                <div class="seg-text" v-html="escapeHtml(item.text)"></div>
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
            <span class="seg-speaker interim-speaker" :style="{ color: 'var(--color-warning)' }">{{ interimSpeakerName }}</span>
            <span class="seg-time">识别中…</span>
          </div>
          <div class="seg-text interim-text" v-html="escapeHtml(interimText)"></div>
        </div>

        <!--
          框选浮标：生效后锚定在选中文字上方（拖动过程中不出现任何浮层，只有文字高亮）。
          mousedown.prevent 阻止点击时浏览器清除选区，否则浮标会在 click 之前消失。
        -->
        <div
          v-if="selection && selectionPos && !holding"
          class="selection-action"
          :style="{ left: `${selectionPos.x}px`, top: `${selectionPos.y}px` }"
          @mousedown.prevent
        >
          <button
            type="button"
            class="selection-btn"
            title="把选中文字对应的音频注册为新说话人"
            @click="openSpeakerModal"
          >
            <UserAddOutlined />
            添加为说话人
          </button>
          <span class="selection-meta">
            <CheckCircleFilled class="meta-check" />
            {{ selection.count }} 句 · {{ selectedDuration.toFixed(1) }}s
          </span>
          <button
            type="button"
            class="selection-dismiss"
            title="取消框选"
            aria-label="取消框选"
            @click="dismissSelection"
          >
            <CloseOutlined />
          </button>
        </div>
      </div>
    </a-card>

    <!-- 添加说话人：框选片段回传后端截取音频注册声纹 -->
    <a-modal
      v-model:open="speakerModalOpen"
      title="添加说话人"
      :width="440"
      :mask-closable="false"
      :confirm-loading="registering"
      ok-text="注册说话人"
      cancel-text="取消"
      @ok="submitSpeakerRegistration"
      @cancel="closeSpeakerModal"
    >
      <div class="register-form">
        <a-alert
          type="info"
          show-icon
          :message="`已框选 ${modalRange?.count ?? 0} 句，约 ${modalDuration.toFixed(1)} 秒`"
          description="系统将从这段音频中提取声纹，注册后本次会议中该说话人的后续发言会被自动识别与区分。"
        />
        <a-form layout="vertical" class="register-fields">
          <a-form-item label="说话人名称" required>
            <a-input
              v-model:value="speakerForm.name"
              placeholder="例如：王小明"
              :maxlength="32"
              show-count
              allow-clear
              @press-enter="submitSpeakerRegistration"
            />
          </a-form-item>
          <a-form-item label="描述（可选）">
            <a-input
              v-model:value="speakerForm.description"
              placeholder="例如：技术部产品经理"
              :maxlength="50"
              allow-clear
            />
          </a-form-item>
        </a-form>
        <div v-if="modalRange" class="register-preview">
          <span class="preview-label">选中内容</span>
          <p class="preview-text">{{ modalRange.text }}</p>
        </div>
        <a-alert v-if="registerError" type="error" show-icon :message="registerError" />
      </div>
    </a-modal>

    <a-drawer v-model:open="drawerOpen" title="会议信息" :size="480" placement="right">
      <div class="drawer-body">
        <!-- 会议概要：从顶部栏移出的会议属性 -->
        <div class="meeting-brief">
          <div class="brief-row">
            <CalendarOutlined />
            <span class="brief-label">会议时间</span>
            <span class="brief-value">{{ meetingTimeLabel || '未设置' }}</span>
          </div>
          <div class="brief-row">
            <component :is="recordMode === 'mic' ? AudioOutlined : SoundOutlined" />
            <span class="brief-label">录音方式</span>
            <span class="brief-value">{{ recordMode === 'mic' ? '麦克风录音' : '系统内录' }}</span>
          </div>
          <div class="brief-row">
            <TeamOutlined />
            <span class="brief-label">参会人数</span>
            <span class="brief-value">{{ participants.length }} 人</span>
          </div>
        </div>

        <!-- 三类会议配置在这里平级切换，顶部栏无需再放三个入口按钮 -->
        <a-radio-group v-model:value="drawerKey" button-style="solid" class="drawer-switch">
          <a-radio-button value="participants">参会人员 {{ participants.length }}</a-radio-button>
          <a-radio-button value="speakers">说话人 {{ speakers.length }}</a-radio-button>
          <a-radio-button value="hotWords">热词库 {{ selectedLibraries.length }}</a-radio-button>
        </a-radio-group>

        <!-- 参会人员 -->
        <div v-if="drawerKey === 'participants'" class="detail-list">
          <div v-for="name in filteredParticipants" :key="name" class="detail-item">
            <div class="detail-main">
              <div class="detail-title">
                <span>{{ name }}</span>
              </div>
            </div>
          </div>
          <a-empty v-if="filteredParticipants.length === 0" description="暂无匹配的参会人员" />
        </div>

        <!-- 说话人 -->
        <div v-else-if="drawerKey === 'speakers'" class="detail-list">
          <div v-for="s in filteredSpeakers" :key="s.id" class="detail-item">
            <div class="detail-main">
              <div class="detail-title">
                <span>{{ s.name }}</span>
              </div>
            </div>
          </div>
          <a-empty v-if="filteredSpeakers.length === 0" description="暂无匹配的说话人" />
        </div>

        <!-- 热词库 -->
        <div v-else class="detail-list">
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
      </div>
    </a-drawer>
  </div>
</template>

<style scoped>
.live-transcribe {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: calc(100vh - var(--titlebar-height));
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
  gap: 16px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 12px 16px;
  box-shadow: var(--shadow-card);
}
.meeting-info {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 auto;
  min-width: 0;
}
.back-btn {
  flex: none;
  color: var(--color-text-secondary);
}
.back-btn:hover {
  color: var(--color-brand-hover) !important;
}
.info-main {
  min-width: 0;
}
.info-main h2 {
  margin: 0;
  color: var(--color-text);
  font-size: 17px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.info-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 3px;
  color: var(--color-text-muted);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
}
.meta-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex: none;
}
/* 时长是会议进行中的实时锚点：等宽数字避免每秒跳动 */
.elapsed-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex: none;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--color-surface-2);
  color: var(--color-text);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.top-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
/* 分隔「查看会议信息」与「控制转写」两组操作 */
.actions-divider {
  width: 1px;
  height: 18px;
  margin: 0 2px;
  background: var(--color-border);
}
.icon-btn {
  padding-inline: 8px;
}
.volume-meter {
  display: inline-block;
  width: 56px;
  height: 6px;
  border-radius: 3px;
  background: var(--color-surface-2);
  overflow: hidden;
}
/* 窗口变窄时优先保住操作区：录音方式退化为纯图标 */
@media (max-width: 1040px) {
  .record-label {
    display: none;
  }
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
/* 会议概要：承载从顶部栏移出的会议属性 */
.meeting-brief {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm, 8px);
  background: var(--color-surface-2);
}
.brief-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.brief-label {
  color: var(--color-text-muted);
}
.brief-value {
  color: var(--color-text);
  font-weight: 500;
}
/* 三类会议配置平级切换，按钮等宽铺满 */
.drawer-switch {
  display: flex;
  width: 100%;
}
.drawer-switch :deep(.ant-radio-button-wrapper) {
  flex: 1 1 0;
  padding-inline: 6px;
  text-align: center;
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
  box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-success) 60%, transparent);
  animation: pulse 1.4s infinite;
}
.live-dot.paused {
  background: var(--color-warning);
  animation: none;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-success) 60%, transparent); }
  70% { box-shadow: 0 0 0 8px transparent; }
  100% { box-shadow: 0 0 0 0 transparent; }
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

/* ---- 框选交互提示：让「按住拖动 + 持续数秒」的规则可见 ---- */
.select-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  /* 固定行高占位：淡出时不改变布局，框选期间列表不会上下跳动 */
  height: 18px;
  line-height: 18px;
  margin-bottom: 10px;
  color: var(--color-text-muted);
  font-size: 12px;
  user-select: none;
  -webkit-user-select: none;
  transition: opacity 0.18s ease;
}

/* 框选期间与已选中时收起提示，但保留占位 */
.select-hint.is-muted {
  opacity: 0;
}

/* ---- 文字高亮：浏览器原生选区改为品牌色半透明底色，与片段级边框共同构成高亮 ---- */
.seg-text::selection,
.seg-speaker::selection,
.seg-time::selection {
  background: color-mix(in srgb, var(--color-brand) 30%, transparent);
  color: inherit;
}

/* ---- 片段级高亮：左侧色条 + 浅底色，区分「进行中 / 已生效 / 未达标」 ---- */
.transcript-item.is-selecting {
  background: color-mix(in srgb, var(--color-brand) 9%, transparent);
  box-shadow: inset 3px 0 0 var(--color-brand);
}

.transcript-item.is-selected {
  background: color-mix(in srgb, var(--color-success) 11%, transparent);
  box-shadow: inset 3px 0 0 var(--color-success);
}

/* 生效瞬间：外扩一圈成功色光晕，明确「已生效」 */
.transcript-item.is-selected.is-confirm {
  animation: select-confirm 0.62s cubic-bezier(0.4, 0, 0.2, 1) 1;
}

/* 未达标：错误色 + 轻微横向抖动 */
.transcript-item.is-rejected {
  background: color-mix(in srgb, var(--color-error) 10%, transparent);
  box-shadow: inset 3px 0 0 var(--color-error);
  animation: select-reject 0.4s ease-in-out 1;
}

@keyframes select-confirm {
  0% {
    box-shadow:
      inset 3px 0 0 var(--color-success),
      0 0 0 0 color-mix(in srgb, var(--color-success) 45%, transparent);
  }
  100% {
    box-shadow:
      inset 3px 0 0 var(--color-success),
      0 0 0 12px transparent;
  }
}

@keyframes select-reject {
  0%,
  100% {
    transform: translateX(0);
  }
  20% {
    transform: translateX(-3px);
  }
  50% {
    transform: translateX(3px);
  }
  80% {
    transform: translateX(-2px);
  }
}

/* ---- 框选浮标：贴着选中的转写文字，提供「添加为说话人」入口 ---- */
.selection-action {
  position: fixed;
  z-index: 30;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 4px;
  border: 1px solid var(--color-border);
  border-radius: 999px;
  background: var(--color-surface);
  box-shadow: var(--shadow-sm);
  transform: translate(-50%, -100%);
  animation: selection-pop 0.16s cubic-bezier(0.4, 0, 0.2, 1) forwards;
}

@keyframes selection-pop {
  from {
    opacity: 0;
    transform: translate(-50%, -92%) scale(0.96);
  }
  to {
    opacity: 1;
    transform: translate(-50%, -100%) scale(1);
  }
}

.selection-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 12px;
  border: none;
  border-radius: 999px;
  background: var(--color-brand);
  color: #fff;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
  transition: background 0.16s ease, opacity 0.16s ease;
}

.selection-btn:hover {
  background: var(--color-brand-hover);
}

.selection-btn:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 2px;
}

.selection-meta {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--color-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.meta-check {
  color: var(--color-success);
  font-size: 12px;
}

/* 取消框选：让「已生效」的高亮随时可撤下 */
.selection-dismiss {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 11px;
  line-height: 0;
  cursor: pointer;
  transition: background 0.16s ease, color 0.16s ease;
}

.selection-dismiss:hover {
  background: var(--color-surface-2);
  color: var(--color-text);
}

.selection-dismiss:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 1px;
}

/* 弱动效偏好下保留状态色与边框，仅去掉动画 */
@media (prefers-reduced-motion: reduce) {
  .transcript-item.is-selected.is-confirm,
  .transcript-item.is-rejected {
    animation: none;
  }
}

/* ---- 添加说话人弹窗 ---- */
.register-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.register-fields {
  margin-bottom: -12px;
}

.register-preview {
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm, 8px);
  background: var(--color-surface-2);
}

.preview-label {
  color: var(--color-text-muted);
  font-size: 12px;
}

.preview-text {
  margin: 6px 0 0;
  max-height: 84px;
  overflow-y: auto;
  color: var(--color-text);
  font-size: 13px;
  line-height: 1.7;
  word-break: break-word;
}
</style>
