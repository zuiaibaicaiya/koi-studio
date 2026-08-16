import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router';
import { useAuthStore } from '../store/auth';

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/Login.vue'),
    meta: { public: true },
  },
  {
    path: '/home',
    name: 'home',
    component: () => import('../views/Home.vue'),
    meta: { requiresAuth: true, title: '首页' },
  },
  {
    path: '/system',
    component: () => import('../layout/BasicLayout.vue'),
    meta: { requiresAuth: true, title: '系统管理' },
    redirect: '/system/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('../views/system/Dashboard.vue'),
        meta: { title: '仪表盘' },
      },
      {
        path: 'users',
        name: 'users',
        component: () => import('../views/system/UserManagement.vue'),
        meta: { title: '用户管理' },
      },
      {
        path: 'hot-words',
        name: 'hotWords',
        component: () => import('../views/system/HotWordManagement.vue'),
        meta: { title: '热词库管理' },
      },
      {
        path: 'speakers',
        name: 'speakers',
        component: () => import('../views/system/SpeakerManagement.vue'),
        meta: { title: '说话人管理' },
      },
      {
        path: 'meetings',
        name: 'meetings',
        component: () => import('../views/system/MeetingManagement.vue'),
        meta: { title: '会议管理' },
      },
    ],
  },
  // 实时会议：独立路由，不依赖 /system 布局分组
  {
    path: '/live/create',
    name: 'liveCreate',
    component: () => import('../views/meeting/LiveMeetingCreate.vue'),
    meta: { requiresAuth: true, title: '创建实时会议' },
  },
  {
    path: '/live/transcribe',
    name: 'liveTranscribe',
    component: () => import('../views/meeting/LiveMeetingTranscribe.vue'),
    meta: { requiresAuth: true, title: '实时转写' },
  },
  // 会议详情：独立路由，采用虚拟列表加载转写内容
  {
    path: '/meeting/detail/:id',
    name: 'meetingDetail',
    component: () => import('../views/meeting/MeetingDetail.vue'),
    meta: { requiresAuth: true, title: '会议详情' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/home',
  },
];

const router = createRouter({
  history: createWebHashHistory(),
  routes,
});

router.beforeEach((to) => {
  const auth = useAuthStore();

  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } };
  }

  if (to.meta.public && auth.isAuthenticated) {
    return { name: 'home' };
  }
});

export default router;
