<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { message, Modal } from 'antdv-next';
import { useSpeakerStore, type Speaker } from '../../store/speaker';
import { hotWordApi } from '../../services/hotWordApi';
import type { HotWordDTO } from '../../services/hotWordApi';
import { meetingApi } from '../../services/meetingApi';
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
  SearchOutlined,
  CalendarOutlined,
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';

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
const participantNames = (route.query.participants as string)?.split(',').filter(Boolean) || [];
const speakerIds = (route.query.speakers as string)?.split(',').filter(Boolean).map(Number) || [];
const hotWordIds = (route.query.hotWords as string)?.split(',').filter(Boolean).map(Number) || [];
const startTime = (route.query.startTime as string) || '';
const endTime = (route.query.endTime as string) || '';
const meetingId = (route.query.meetingId as string) || '';
const meetingTimeLabel = computed(() => {
  if (!startTime) return '';
  const fmt = (s: string) =>
    new Date(s).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  return endTime ? `${fmt(startTime)} ~ ${fmt(endTime)}` : fmt(startTime);
});

const participants = computed(() => participantNames);
const speakers = computed<Speaker[]>(() =>
  speakerIds.map((id) => speakerStore.getById(id)).filter((s): s is Speaker => !!s),
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
  if (hotWordIds.length === 0) {
    selectedLibraries.value = [];
    return;
  }
  hotWordLoading.value = true;
  try {
    const res = await hotWordApi.listLibraries({ pageSize: 1000 });
    const chosen = res.items.filter((lib) => hotWordIds.includes(lib.id));
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
const hotWordSet = computed(() => new Set(hotWords.value.map((w) => w.word)));

let timer: number | undefined;
let segId = 0;

const sampleTexts = [
  '我们先来回顾一下上周的项目进展，整体节奏比预期要快一些，几个核心模块都已经进入了联调阶段。',
  '实时转写功能已经接入了最新的语音识别模型，中文普通话的识别准确率在安静环境下已经可以稳定在 97% 以上，而且端到端的延迟控制在 800 毫秒以内，基本能做到“边说边出字”。',
  '热词库对专业术语的识别准确率有明显提升。',
  '麦克风录音和系统内录两种方式都可以稳定工作，不过在多人同时发言、会议室回声较大的场景下，系统内录的信噪比会更好一点。',
  '接下来讨论一下下个版本的迭代计划：第一优先级是把实时转写沉淀成会议纪要，第二优先级是多语种混合识别，第三优先级是离线模式，方便在没有网络的现场会议里也能用。',
  '说话人分离的效果比预期要好很多，五人以内的会议基本能准确区分每个人，但超过八人之后偶发会串音，后续需要引入声纹聚类来做兜底。',
  '建议在会议结束后自动生成结构化纪要，包含议程、决议、待办、负责人和截止时间五个板块，并支持一键导出成 Markdown 和 Word。',
  '用户反馈希望支持更多方言的实时识别，比如粤语、四川话、闽南语，这部分我们和算法团队约了下周做一次可行性评估。',
  '关于权限这块，建议区分“查看者 / 编辑者 / 管理员”三种角色，查看者只能浏览转写结果，编辑者可以修正文本和说话人，管理员可以管理热词和参会人员。',
  '数据安全的同学提了一个点：转写音频默认在本地处理、不上传云端，只有用户主动开启“云端增强识别”时才会上传，而且上传的内容要做脱敏和加密。',
  '短句。',
  '性能方面，单场会议累积到上千条转写时，普通列表会出现明显卡顿，所以我们打算换成虚拟滚动来只渲染可视区域，理论上列表再长也不会掉帧。',
  '还有一件小事——很多同事习惯在会议里用缩写，比如把“客户成功”叫成“客成”，这类内部黑话我们可以维护一份同义词热词，识别之后再自动展开成完整表述。',
  '最后同步一个排期：灰度会在下周二先对内部 20 人开放，收集一轮反馈后，月底再对全公司开放，正式 GA 预计在季度末。',
];

function nowTime(base?: Date) {
  const d = base ?? new Date();
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
}

function makeSegment(at?: Date): Segment {
  const sp = speakers.value.length
    ? speakers.value[Math.floor(Math.random() * speakers.value.length)]
    : { id: -1, name: '说话人' };
  const text = sampleTexts[Math.floor(Math.random() * sampleTexts.length)];
  return {
    id: segId++,
    speakerId: sp.id,
    speakerName: sp.name,
    text,
    time: nowTime(at),
  };
}

function pushSegment() {
  if (speakers.value.length === 0) return;
  segments.value.push(makeSegment());
  scrollToBottom();
}

const transcriptScrollerRef = ref<{ scrollToBottom: () => void } | null>(null);
const autoScroll = ref(true);

function onTranscriptScroll(e: Event) {
  const el = e.target as HTMLElement;
  autoScroll.value = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}
function scrollToBottom() {
  nextTick(() => {
    if (autoScroll.value && transcriptScrollerRef.value) {
      transcriptScrollerRef.value.scrollToBottom();
    }
  });
}

function tick() {
  if (!running.value) return;
  elapsed.value += 1;
  if (elapsed.value % 3 === 0) pushSegment();
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
  message.info(running.value ? '已继续转写' : '已暂停转写');
}

function backToCreate() {
  router.push({ name: 'liveCreate' });
}

function stopMeeting() {
  Modal.confirm({
    title: '结束会议',
    content: '确认结束本次实时会议？结束后将跳转首页。',
    okText: '结束',
    cancelText: '取消',
    onOk: async () => {
      running.value = false;
      stopTimer();
      if (meetingId) {
        try {
          await meetingApi.finishMeeting(Number(meetingId));
        } catch (err) {
          message.warning((err as Error)?.message || '标记会议结束失败');
        }
      }
      router.push({ name: 'home' });
    },
  });
}

// 热词高亮
const highlightEnabled = ref(true);

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
  let html = escapeHtml(text);
  if (!highlightEnabled.value) return html;
  hotWordSet.value.forEach((w) => {
    if (!w) return;
    const safe = escapeHtml(w);
    html = html.split(safe).join(`<mark class="hot-word">${safe}</mark>`);
  });
  return html;
}

/* ------------------------- 顶部按钮 -> 侧边抽屉 ------------------------- */
type DrawerKey = 'participants' | 'speakers' | 'hotWords';

const drawerOpen = ref(false);
const drawerKey = ref<DrawerKey>('participants');
const keyword = ref('');

const drawerTitleMap: Record<DrawerKey, string> = {
  participants: '参会人员',
  speakers: '说话人',
  hotWords: '热词库',
};
const drawerTitle = computed(() => drawerTitleMap[drawerKey.value]);

function openDrawer(key: DrawerKey) {
  drawerKey.value = key;
  keyword.value = '';
  drawerOpen.value = true;
}

const lowerKeyword = computed(() => keyword.value.trim().toLowerCase());

const filteredParticipants = computed(() => {
  const kw = lowerKeyword.value;
  if (!kw) return participants.value;
  return participants.value.filter((name) => name.toLowerCase().includes(kw));
});

const filteredSpeakers = computed(() => {
  const kw = lowerKeyword.value;
  if (!kw) return speakers.value;
  return speakers.value.filter((s) =>
    [s.name, s.language, s.gender, s.description].some((f) => f.toLowerCase().includes(kw)),
  );
});

// 各说话人发言段数
const speakerSegmentCount = computed(() => {
  const map = new Map<number, number>();
  segments.value.forEach((s) => map.set(s.speakerId, (map.get(s.speakerId) ?? 0) + 1));
  return map;
});

function speakerShare(id: number) {
  const total = segments.value.length;
  if (!total) return 0;
  return Math.round(((speakerSegmentCount.value.get(id) ?? 0) / total) * 100);
}

// 各热词命中次数（按词匹配，因热词库接口返回的热词仅有 word/weight）
const hotWordHitCount = computed(() => {
  const all = segments.value.map((s) => s.text).join('\n');
  const map = new Map<string, number>();
  hotWords.value.forEach((w) => {
    map.set(w.word, w.word ? all.split(w.word).length - 1 : 0);
  });
  return map;
});

// 按热词库分组热词
const hotWordGroups = computed(() => {
  const kw = lowerKeyword.value;
  return selectedLibraries.value.map((lib) => ({
    name: lib.name,
    words: kw ? lib.words.filter((w) => w.word.toLowerCase().includes(kw)) : lib.words,
  }));
});

// 只看某位说话人的发言
const focusSpeakerId = ref<number | null>(null);
const focusSpeakerName = computed(
  () => speakers.value.find((s) => s.id === focusSpeakerId.value)?.name ?? '',
);
function toggleFocusSpeaker(id: number) {
  focusSpeakerId.value = focusSpeakerId.value === id ? null : id;
}
const visibleSegments = computed(() =>
  focusSpeakerId.value === null
    ? segments.value
    : segments.value.filter((s) => s.speakerId === focusSpeakerId.value),
);

const elapsedText = computed(() => {
  const m = Math.floor(elapsed.value / 60);
  const s = elapsed.value % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
});

// 切换会议时重置
watch(
  () => route.query,
  (q) => {
    meetingName.value = (q.name as string) || '未命名会议';
    recordMode.value = (q.recordMode as 'mic' | 'system') || 'mic';
    segments.value = [];
    elapsed.value = 0;
    segId = 0;
    running.value = true;
    focusSpeakerId.value = null;
    startTimer();
    loadHotWords();
    markMeetingOngoing();
  },
);

onMounted(() => {
  startTimer();
  // 加载说话人列表，供 getById 检索转写参与者
  speakerStore.load();
  // 加载所选热词库及其热词
  loadHotWords();
  markMeetingOngoing();
});

/** 进入转写页即标记会议为进行中（实时会议）。 */
function markMeetingOngoing() {
  if (!meetingId) return;
  meetingApi
    .startMeeting(Number(meetingId))
    .catch((err) => message.warning((err as Error)?.message || '标记会议进行中失败'));
}
onBeforeUnmount(() => {
  stopTimer();
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
        <a-button @click="togglePause">
          <template #icon>
            <component :is="running ? PauseCircleOutlined : PlayCircleOutlined" />
          </template>
          {{ running ? '暂停' : '继续' }}
        </a-button>
        <a-button danger @click="stopMeeting"><StopOutlined />结束</a-button>
      </div>
    </div>

    <a-card class="transcript-card" variant="borderless">
      <template #title>
        <span class="live-title">
          <span class="live-dot" :class="{ paused: !running }"></span>
          实时转写
          <a-tag
            v-if="focusSpeakerId !== null"
            color="processing"
            closable
            @close="focusSpeakerId = null"
          >
            仅看 {{ focusSpeakerName }}
          </a-tag>
        </span>
      </template>
      <div class="transcript-list">
        <div v-if="visibleSegments.length === 0" class="transcript-empty">
          <AudioOutlined />
          <p v-if="focusSpeakerId !== null">该说话人暂无发言内容</p>
          <p v-else>正在聆听… 转写内容将实时显示在这里</p>
        </div>
        <DynamicScroller
          v-else
          ref="transcriptScrollerRef"
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
      </div>
    </a-card>

    <a-drawer
      v-model:open="drawerOpen"
      :title="drawerTitle"
      :size="460"
      placement="right"
    >
      <div class="drawer-body">
        <a-input
          v-model:value="keyword"
          allow-clear
          :placeholder="
            drawerKey === 'participants'
              ? '搜索参会人员姓名'
              : drawerKey === 'speakers'
                ? '搜索说话人 / 语种'
                : '搜索热词 / 分类'
          "
        >
          <template #prefix><SearchOutlined /></template>
        </a-input>

        <!-- 参会人员 -->
        <template v-if="drawerKey === 'participants'">
          <div class="drawer-summary">
            <span>共 {{ participants.length }} 人</span>
          </div>
          <div class="detail-list">
            <div v-for="name in filteredParticipants" :key="name" class="detail-item">
              <a-avatar :size="38" :style="{ backgroundColor: 'var(--color-brand)' }">
                {{ name.charAt(0) }}
              </a-avatar>
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
          <div class="drawer-summary">
            <span>共 {{ speakers.length }} 位说话人</span>
            <span>已转写 {{ segments.length }} 段</span>
          </div>
          <div class="detail-list">
            <div v-for="s in filteredSpeakers" :key="s.id" class="detail-item">
              <a-avatar :size="38" :style="{ backgroundColor: 'var(--color-success)' }">
                {{ s.name.charAt(0) }}
              </a-avatar>
              <div class="detail-main">
                <div class="detail-title">
                  <span>{{ s.name }}</span>
                  <a-tag color="blue">{{ s.language }}</a-tag>
                  <a-tag>{{ s.gender }}</a-tag>
                </div>
                <div class="detail-sub">{{ s.description }}</div>
                <div class="detail-sub">
                  声纹样本 {{ s.sampleCount }} 条
                  <span class="dot-split">·</span>
                  注册于 {{ s.createdAt }}
                </div>
                <div class="detail-metric">
                  <span class="metric-label">
                    本场发言 {{ speakerSegmentCount.get(s.id) || 0 }} 段（{{ speakerShare(s.id) }}%）
                  </span>
                  <a-progress :percent="speakerShare(s.id)" size="small" :show-info="false" />
                </div>
                <a-button
                  size="small"
                  :type="focusSpeakerId === s.id ? 'primary' : 'default'"
                  @click="toggleFocusSpeaker(s.id)"
                >
                  {{ focusSpeakerId === s.id ? '取消筛选' : '只看 TA 的发言' }}
                </a-button>
              </div>
            </div>
            <a-empty v-if="filteredSpeakers.length === 0" description="暂无匹配的说话人" />
          </div>
        </template>

        <!-- 热词库 -->
        <template v-else>
          <div class="config-row">
            <div>
              <div class="config-label">转写中高亮热词</div>
              <div class="detail-sub">开启后命中的热词会在转写文本中标黄显示</div>
            </div>
            <a-switch v-model:checked="highlightEnabled" />
          </div>
          <div class="drawer-summary">
            <span>共 {{ selectedLibraries.length }} 个热词库</span>
            <span>{{ hotWords.length }} 个热词</span>
          </div>
          <div class="detail-list">
            <div v-for="g in hotWordGroups" :key="g.name" class="word-group">
              <div class="group-title">
                {{ g.name }}
                <span class="group-count">{{ g.words.length }}</span>
              </div>
              <div v-for="w in g.words" :key="w.word" class="detail-item">
                <div class="detail-main">
                  <div class="detail-title">
                    <span>{{ w.word }}</span>
                    <a-tag color="gold">权重 {{ w.weight }}</a-tag>
                    <a-tag :color="(hotWordHitCount.get(w.word) || 0) > 0 ? 'green' : 'default'">
                      命中 {{ hotWordHitCount.get(w.word) || 0 }} 次
                    </a-tag>
                  </div>
                  <div class="detail-metric">
                    <span class="metric-label">识别权重</span>
                    <a-progress :percent="w.weight" size="small" :show-info="false" />
                  </div>
                </div>
              </div>
            </div>
            <a-empty v-if="hotWords.length === 0" description="暂无匹配的热词" />
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
  flex-wrap: wrap;
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
  flex-wrap: wrap;
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
.drawer-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--color-text-secondary);
  font-size: 12px;
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
.seg-text :deep(.hot-word) {
  background: rgba(250, 173, 20, 0.18);
  color: #d48806;
  padding: 0 2px;
  border-radius: 3px;
}
</style>
