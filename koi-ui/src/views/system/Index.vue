<script setup lang="ts">
import {
  TeamOutlined,
  UserOutlined,
  AppstoreOutlined,
  SafetyOutlined,
} from '@antdv-next/icons';

const stats = [
  { title: '用户总数', value: 128, icon: UserOutlined },
  { title: '在线设备', value: 36, icon: AppstoreOutlined },
  { title: '组织架构', value: 8, icon: TeamOutlined },
  { title: '权限策略', value: 15, icon: SafetyOutlined },
];
</script>

<template>
  <div class="dashboard">
    <div class="header">
      <h2>系统管理</h2>
      <p class="sub">系统概览与配置中心</p>
    </div>

    <div class="stats">
      <a-card v-for="item in stats" :key="item.title" class="stat-card" variant="borderless">
        <div class="stat">
          <component :is="item.icon" class="stat-icon" />
          <div>
            <div class="stat-value">{{ item.value }}</div>
            <div class="stat-title">{{ item.title }}</div>
          </div>
        </div>
      </a-card>
    </div>

    <a-card title="快捷入口" class="panel" variant="borderless">
      <a-space wrap>
        <a-button type="primary">用户管理</a-button>
        <a-button>角色权限</a-button>
        <a-button>设备列表</a-button>
        <a-button>系统设置</a-button>
      </a-space>
    </a-card>
  </div>
</template>

<style scoped>
.dashboard {
  max-width: 960px;
  margin: 0 auto;
  padding: 32px 16px;
}

.header h2 {
  margin: 0;
  color: var(--color-text);
}

.sub {
  margin: 4px 0 0;
  color: var(--color-text-muted);
}

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
  margin: 24px 0;
}

.stat {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  font-size: 32px;
  color: var(--color-brand);
}

.stat-value {
  font-size: 28px;
  font-weight: 600;
  line-height: 1.1;
  color: var(--color-text);
}

.stat-title {
  color: var(--color-text-muted);
  font-size: 13px;
}
</style>
