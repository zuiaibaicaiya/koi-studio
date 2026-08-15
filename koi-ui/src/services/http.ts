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

/** 后端基地址：优先读取 PUBLIC_API_BASE（Rsbuild 公共前缀），默认指向本地后端。 */
const BASE_URL = (import.meta.env.PUBLIC_API_BASE as string | undefined) ?? 'http://127.0.0.1:8000';

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

/**
 * 防重复锁：并发的多个 401 只会触发一次登出跳转。
 * 跳转到登录页完成（或被导航守卫拦截）后才释放，避免在本次会话内重复跳转。
 */
let unauthorizedHandled: Promise<void> | null = null;

/** 登录失效时清除本地会话并跳转登录页（带防重复处理）。 */
function handleUnauthorized(): Promise<void> {
  if (unauthorizedHandled) return unauthorizedHandled;

  unauthorizedHandled = (async () => {
    // 1. 清除本地存储中的 token 与用户信息（pinia persist 会同步到 localStorage）
    const auth = useAuthStore();
    auth.clearSession();

    // 2. 跳转到登录页，并通过 redirect 记录用户原本访问的页面
    const { default: router } = await import('../router');
    const current = router.currentRoute.value;
    if (current.name !== 'login') {
      await router.replace({
        name: 'login',
        query: current.fullPath ? { redirect: current.fullPath } : {},
      });
    }
  })().finally(() => {
    unauthorizedHandled = null;
  });

  return unauthorizedHandled;
}

/** 判断响应是否为 token 过期（HTTP 401 或业务码 401 的过期标识）。 */
function isTokenExpired(response: Response, envelope?: ApiEnvelope): boolean {
  if (response.status === 401) return true;
  // 兜底：部分实现可能以 200 + code:401 返回过期，统一视为登录失效
  if (envelope && envelope.code === 401) return true;
  return false;
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

  // token 过期：HTTP 401 直接判定（无需解析响应体），统一跳转登录页
  if (response.status === 401) {
    let msg = '登录已过期，请重新登录';
    try {
      const ct = response.headers.get('content-type') ?? '';
      if (ct.includes('application/json')) {
        const env = (await response.json()) as ApiEnvelope;
        if (env?.msg) msg = env.msg;
      }
    } catch {
      // 解析失败时使用默认提示
    }
    await handleUnauthorized();
    throw new ApiError(msg, 401);
  }

  // 其余响应统一解析 JSON 响应体
  const contentType = response.headers.get('content-type') ?? '';
  const isJson = contentType.includes('application/json');
  const envelope: ApiEnvelope<T> | undefined = isJson
    ? ((await response.json()) as ApiEnvelope<T>)
    : undefined;

  if (!response.ok) {
    throw new ApiError(`请求失败（HTTP ${response.status}）`, response.status);
  }

  // 兜底：业务码层面的 token 过期标识（与 HTTP 401 等价）
  if (isTokenExpired(response, envelope)) {
    await handleUnauthorized();
    throw new ApiError(envelope?.msg ?? '登录已过期，请重新登录', 401);
  }

  if (envelope && envelope.code !== 0) {
    throw new ApiError(envelope.msg || '请求失败', envelope.code);
  }

  return envelope?.data as T;
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
