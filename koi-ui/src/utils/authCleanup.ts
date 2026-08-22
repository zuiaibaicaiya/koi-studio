/**
 * 认证失效时的本地存储清理与登录页跳转工具。
 *
 * 当 token 过期 / 无效（HTTP 401 或业务码 401）时，需要：
 * 1. 彻底清除前端本地存储中的认证相关信息（localStorage / sessionStorage / Cookie）；
 * 2. 自动跳转登录页，并携带 redirect 以便登录成功后回到原页面；
 * 3. 已处于登录页时避免重复跳转。
 */

/** auth store 经 pinia-plugin-persistedstate 持久化到 localStorage 的 key（persist: true 时默认取 store id）。 */
export const AUTH_STORE_KEY = 'auth';

/** localStorage / sessionStorage 中其它可能存在的认证相关 key 前缀（兜底匹配，避免误删主题等非认证数据）。 */
const AUTH_KEY_PATTERN = /^(token|auth|session|credential|login)/i;

/** 清理指定存储（localStorage / sessionStorage）中与认证相关的 key。 */
function clearStorageAuth(storage: Storage | undefined): void {
  if (!storage) return;
  const keys: string[] = [];
  for (let i = 0; i < storage.length; i += 1) {
    const key = storage.key(i);
    if (key && (key === AUTH_STORE_KEY || AUTH_KEY_PATTERN.test(key))) {
      keys.push(key);
    }
  }
  keys.forEach((key) => storage.removeItem(key));
}

/** 清理 localStorage 中的认证相关数据。 */
function clearLocalStorageAuth(): void {
  if (typeof localStorage === 'undefined') return;
  clearStorageAuth(localStorage);
}

/** 清理 sessionStorage 中的认证相关数据。 */
function clearSessionStorageAuth(): void {
  if (typeof sessionStorage === 'undefined') return;
  clearStorageAuth(sessionStorage);
}

/**
 * 清除当前站点可访问的 Cookie。
 * 注意：HttpOnly Cookie 无法经 JS 清除（Electron 场景需主进程配合），此处尽力而为。
 */
function clearCookies(): void {
  if (typeof document === 'undefined') return;
  document.cookie.split(';').forEach((pair) => {
    const name = pair.split('=')[0]?.trim();
    if (!name) return;
    try {
      document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`;
    } catch {
      // 忽略无法清除的 cookie（如 HttpOnly 或受安全策略限制）
    }
  });
}

/** 清除所有认证相关前端本地存储（localStorage / sessionStorage / Cookie）。幂等，可重复调用。 */
export function clearAuthStorage(): void {
  clearLocalStorageAuth();
  clearSessionStorageAuth();
  clearCookies();
}

/**
 * 规范化跳转目标：空路径、根路径或登录页自身不携带 redirect，
 * 避免无意义跳转或登录后仍停留在登录页。
 */
export function normalizeRedirect(path: string | undefined): string {
  if (!path || path === '/' || path.startsWith('/login')) return '';
  return path;
}

/** 防重入标志：同一会话内只发起一次跳转。 */
let redirectingToLogin = false;

/**
 * 认证失效时统一跳转登录页，并携带当前页面地址以便登录成功后返回。
 * 已处于登录页时直接返回，不重复跳转。
 */
export async function redirectToLogin(fullPath?: string): Promise<void> {
  // 动态导入避免与 router/index.ts（其静态导入了 auth store）产生循环依赖
  const { default: router } = await import('../router');
  const current = router.currentRoute.value;
  // 已处于登录页：无需重复跳转（此时仅需确认清理已完成）
  if (current.name === 'login') return;
  if (redirectingToLogin) return;

  redirectingToLogin = true;
  try {
    const redirect = normalizeRedirect(fullPath || current.fullPath);
    await router.replace({
      name: 'login',
      query: redirect ? { redirect } : {},
    });
  } finally {
    redirectingToLogin = false;
  }
}
