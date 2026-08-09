<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useAuthStore } from '../store/auth';
import { VideoCameraOutlined, AudioOutlined, SettingOutlined } from '@antdv-next/icons';

const router = useRouter();
const auth = useAuthStore();

const modules = [
  {
    key: 'meeting',
    title: '实时会议',
    desc: '发起或加入实时音视频会议，支持同声转写与字幕。',
    icon: VideoCameraOutlined,
    color: '#1677ff',
    to: 'liveCreate',
  },
  {
    key: 'transcribe',
    title: '音频转写',
    desc: '上传音频文件，自动识别说话人并生成文字稿与热词标记。',
    icon: AudioOutlined,
    color: '#52c41a',
  },
  {
    key: 'system',
    title: '系统管理',
    desc: '管理用户、角色权限、热词库、说话人与会议配置。',
    icon: SettingOutlined,
    color: '#faad14',
    to: 'dashboard',
  },
];

function open(to?: string) {
  if (to) router.push({ name: to });
}

function openModule(m: { to?: string }) {
  open(m.to);
}
</script>

<template>
  <div class="home">
    <div class="hero">
      <h1>欢迎回来，{{ auth.user?.username || '用户' }}！</h1>
      <p>请选择要进入的功能模块</p>
    </div>

    <a-row :gutter="[16, 16]">
      <a-col v-for="m in modules" :key="m.key" :xs="24" :sm="12" :lg="8">
        <a-card class="module-card" hoverable @click="openModule(m)">
          <div class="module-icon" :style="{ background: m.color }">
            <component :is="m.icon" />
          </div>
          <div class="module-title">{{ m.title }}</div>
          <div class="module-desc">{{ m.desc }}</div>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<style scoped>
.home {
  min-height: 100vh;
  padding: 64px 24px;
  background: radial-gradient(
      1200px 600px at 80% -10%,
      var(--color-brand-soft),
      transparent 60%
    ),
    var(--color-bg);
  color: var(--color-text);
}
.hero {
  margin-bottom: 24px;
}
.hero h1 {
  margin: 0;
  color: var(--color-text);
  font-size: 26px;
  font-weight: 700;
  letter-spacing: -0.01em;
}
.hero p {
  margin: 8px 0 0;
  color: var(--color-text-secondary);
}
.module-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  height: 100%;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  transition: transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;
}
.module-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-md);
  border-color: var(--color-brand);
}
.module-icon {
  width: 52px;
  height: 52px;
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 26px;
  color: #fff;
  margin-bottom: 16px;
}
.module-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--color-text);
  margin-bottom: 8px;
}
.module-desc {
  color: var(--color-text-secondary);
  font-size: 13px;
  line-height: 1.6;
  min-height: 42px;
}
</style>
