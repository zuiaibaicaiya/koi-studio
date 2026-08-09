import { useAuthStore } from '../store/auth';

/** 后端统一响应结构（见 base_controller.go）：{ code, msg, data } */
export interface ApiEnvelope<T = unknown> {
  code: number;
  msg: string;
  data: T;
}

export class ApiError extends Error {
  code: number;
  constructor(message: string, code: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
  }
}

/** 后端基地址：优先读取 VITE_API_BASE，默认指向本地后端。 */
const BASE_URL = (import.meta.env.VITE_API_BASE as string | undefined) ?? 'http://127.0.0.1:8000';

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

interface RequestOptions {
  method?: Method;
  /** 查询参数，自动拼接到 URL。 */
  params?: object;
  /** JSON 请求体。 */
  body?: unknown;
  /** 表单 / 文件上传。提供该字段时忽略 body，且以 multipart/form-data 发送。 */
  formData?: FormData;
  signal?: AbortSignal;
}

/** 防止并发 401 时重复跳转登录页。 */
let isRedirectingToLogin = false;

/** 登录失效时清除本地会话并跳转登录页。 */
async function handleUnauthorized() {
  if (isRedirectingToLogin) return;
  isRedirectingToLogin = true;
  try {
    const auth = useAuthStore();
    auth.token = '';
    auth.user = null;
    const { default: router } = await import('../router');
    const current = router.currentRoute.value;
    if (current.name !== 'login') {
      await router.replace({ name: 'login', query: current.fullPath ? { redirect: current.fullPath } : {} });
    }
  } finally {
    // 延迟重置，避免同一次刷新周期内重复触发
    setTimeout(() => {
      isRedirectingToLogin = false;
    }, 500);
  }
}

function buildUrl(path: string, params?: object): string {
  const url = `${BASE_URL}${path.startsWith('/') ? path : `/${path}`}`;
  if (!params) return url;
  const search = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null) search.append(k, String(v));
  });
  const qs = search.toString();
  return qs ? `${url}?${qs}` : url;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', params, body, formData, signal } = options;

  const headers: Record<string, string> = {};
  const auth = useAuthStore();
  if (auth.token) headers.Authorization = `Bearer ${auth.token}`;

  let payload: BodyInit | undefined;
  if (formData) {
    payload = formData; // 浏览器自动设置 multipart/form-data 的 boundary
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }

  const response = await fetch(buildUrl(path, params), {
    method,
    headers,
    body: payload,
    signal,
  });

  if (response.status === 401) {
    await handleUnauthorized();
    throw new ApiError('登录已过期，请重新登录', 401);
  }
  if (!response.ok) {
    throw new ApiError(`请求失败（HTTP ${response.status}）`, response.status);
  }

  const envelope = (await response.json()) as ApiEnvelope<T>;
  if (envelope.code !== 0) {
    throw new ApiError(envelope.msg || '请求失败', envelope.code);
  }
  return envelope.data;
}

export const http = {
  get: <T>(path: string, params?: object, signal?: AbortSignal) =>
    request<T>(path, { method: 'GET', params, signal }),
  post: <T>(path: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(path, { method: 'POST', body, signal }),
  put: <T>(path: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(path, { method: 'PUT', body, signal }),
  delete: <T>(path: string, params?: object, signal?: AbortSignal) =>
    request<T>(path, { method: 'DELETE', params, signal }),
  /** 以 multipart/form-data 上传文件。 */
  upload: <T>(path: string, formData: FormData, signal?: AbortSignal) =>
    request<T>(path, { method: 'POST', formData, signal }),
  raw: { BASE_URL },
};

export default http;
