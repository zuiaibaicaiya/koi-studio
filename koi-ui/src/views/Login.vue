<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { App } from 'antdv-next';
const { message } = App.useApp();
import { UserOutlined, LockOutlined } from '@antdv-next/icons';
import type { Rule } from 'antdv-next';
import { useAuthStore } from '../store/auth';

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

/* 开发模式下预填默认凭据，生产模式留空 */
const isDev = import.meta.env.DEV;
const DEFAULT_USERNAME = 'admin';
const DEFAULT_PASSWORD = 'admin123';

/* ---- 登录表单 ---- */
const loginFormRef = ref();
const loginState = reactive({
  username: isDev ? DEFAULT_USERNAME : '',
  password: isDev ? DEFAULT_PASSWORD : '',
});

const loginRules: Record<string, Rule[]> = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' as const }, { min: 5, message: '用户名至少 5 位', trigger: 'blur' as const }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' as const }, { min: 6, message: '密码至少 6 位', trigger: 'blur' as const }],
};

async function onLoginFinish(values: { username: string; password: string }) {
  try {
    await auth.login(values);
    message.success('登录成功');
    // 校验 redirect 指向有效内部页面：外部 URL / 登录页自身 / 不存在的路由一律回退首页，
    // 保证认证失败后能安全返回原页面
    const redirect = (route.query.redirect as string) || '/home';
    const resolved = router.resolve(redirect);
    const target = resolved.name && resolved.name !== 'login' ? resolved.fullPath : '/home';
    router.replace(target);
  } catch (err) {
    message.error((err as Error).message || '登录失败');
  }
}

function onLoginFailed() {
  message.warning('请先完成表单填写');
}
</script>

<template>
  <div class="login-page">
    <a-card class="login-card" variant="borderless">
      <!-- 登录表单 -->
      <a-form
        ref="loginFormRef"
        :model="loginState"
        :rules="loginRules"
        layout="vertical"
        @finish="onLoginFinish"
        @finish-failed="onLoginFailed"
      >
        <a-form-item label="用户名" name="username">
          <a-input v-model:value="loginState.username" :placeholder="`请输入用户名（默认 ${DEFAULT_USERNAME}）`" size="large">
            <template #prefix><UserOutlined /></template>
          </a-input>
        </a-form-item>

        <a-form-item label="密码" name="password">
          <a-input-password
            v-model:value="loginState.password"
            :placeholder="`请输入密码（默认 ${DEFAULT_PASSWORD}）`"
            size="large"
          >
            <template #prefix><LockOutlined /></template>
          </a-input-password>
        </a-form-item>

        <a-form-item>
          <a-button type="primary" html-type="submit" block size="large" :loading="auth.loading">
            登录
          </a-button>
        </a-form-item>
      </a-form>

      <p class="hint">
        账号由管理员统一分配
      </p>
    </a-card>
  </div>
</template>

<style scoped>
.login-page {
  display: flex;
  min-height: 100vh;
  align-items: center;
  justify-content: center;
  background: radial-gradient(
      900px 500px at 50% -10%,
      var(--color-brand-soft),
      transparent 60%
    ),
    var(--color-bg);
}

.login-card {
  width: 400px;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  background: var(--color-surface);
}

.hint {
  margin: 0;
  text-align: center;
  font-size: 13px;
  color: var(--color-text-muted);
}

.hint a {
  color: var(--color-brand);
  cursor: pointer;
  margin-left: 4px;
}

.hint a:hover {
  color: var(--color-brand-hover);
}
</style>
