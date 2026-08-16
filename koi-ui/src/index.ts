import { createApp } from 'vue';
import { createPinia } from 'pinia';
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import App from './App.vue';
import router from './router';
import { useAuthStore } from './store/auth';
import './index.css';

dayjs.locale('zh-cn');

async function bootstrap() {
  const pinia = createPinia();
  pinia.use(piniaPluginPersistedstate);

  const app = createApp(App).use(pinia).use(router);

  // 启动校验持久化令牌：若令牌已失效则清除本地会话，
  // 路由守卫随后会将其重定向至登录页，避免带着无效令牌进入受保护页面导致接口全部 401。
  const auth = useAuthStore(pinia);
  await auth.init();

  app.mount('#root');
}

bootstrap();
