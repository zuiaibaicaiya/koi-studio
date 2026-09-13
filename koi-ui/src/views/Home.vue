<script setup lang="ts">
import { useRouter } from 'vue-router';
import { VideoCameraOutlined, AudioOutlined, SettingOutlined } from '@antdv-next/icons';

const router = useRouter();

const modules = [
  {
    key: 'meeting',
    title: '实时会议',
    desc: '发起或加入实时音视频会议，支持同声转写与字幕。',
    icon: VideoCameraOutlined,
    color: '#2f54eb',
    to: 'liveCreate',
  },
  {
    key: 'transcribe',
    title: '音频转写',
    desc: '上传音频文件，自动识别说话人并生成文字稿与热词标记。',
    icon: AudioOutlined,
    color: '#52c41a',
    to: 'offlineCreate',
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
  min-height: calc(100vh - var(--titlebar-height));
  display: flex;
  flex-direction: column;
  /* 居中偏上：flex 居中按内容框计算，底部留白多出的部分等效把内容整体上提一半 */
  justify-content: center;
  /* 主题切换 / 账号菜单 / 退出登录均已收敛到全局标题栏，页面只保留模块入口 */
  padding: 32px 24px 136px;
  background: var(--home-bg);
  color: var(--color-text);
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
