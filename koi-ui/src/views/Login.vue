<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { message } from 'antdv-next';
import { UserOutlined, LockOutlined } from '@antdv-next/icons';
import { useAuthStore } from '../store/auth';

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const formRef = ref();
const formState = reactive({
  username: '',
  password: '',
});

const rules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
};

async function onFinish(values: { username: string; password: string }) {
  try {
    await auth.login(values);
    message.success('登录成功');
    const redirect = (route.query.redirect as string) || '/home';
    router.replace(redirect);
  } catch (err) {
    message.error((err as Error).message || '登录失败');
  }
}

function onFinishFailed() {
  message.warning('请先完成表单填写');
}
</script>

<template>
  <div class="login-page">
    <a-card class="login-card" title="Koi Studio 登录">
      <a-form
        ref="formRef"
        :model="formState"
        :rules="rules"
        layout="vertical"
        @finish="onFinish"
        @finish-failed="onFinishFailed"
      >
        <a-form-item label="用户名" name="username">
          <a-input v-model:value="formState.username" placeholder="请输入用户名" size="large">
            <template #prefix><UserOutlined /></template>
          </a-input>
        </a-form-item>

        <a-form-item label="密码" name="password">
          <a-input-password
            v-model:value="formState.password"
            placeholder="请输入密码"
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

      <p class="hint">演示账号：admin / 123456</p>
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
  width: 360px;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  background: var(--color-surface);
}

.hint {
  margin: 0;
  text-align: center;
  font-size: 12px;
  color: var(--color-text-muted);
}
</style>
