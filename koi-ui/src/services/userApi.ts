import http from './http';

/** 后端 User 模型字段 */
export interface UserDTO {
  id: number;
  username: string;
  nickname: string;
  email: string;
  phone: string;
  avatar: string;
  status: string; // "active" | "inactive"
  created_at: string;
  updated_at: string;
}

/** 登录/注册返回 */
export interface AuthResult {
  token: string;
  user: UserDTO;
}

/** 分页列表 */
export interface Paginated<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPage: number;
}

export interface LoginPayload {
  username: string;
  password: string;
}

export interface RegisterPayload {
  username: string;
  password: string;
  nickname?: string;
  email?: string;
  phone?: string;
}

export interface UpdateProfilePayload {
  nickname?: string;
  email?: string;
  phone?: string;
  avatar?: string;
}

export interface ChangePasswordPayload {
  oldPassword: string;
  newPassword: string;
}

export interface UserListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
  status?: string;
}

export interface CreateUserPayload {
  username: string;
  password: string;
  nickname?: string;
  email?: string;
  phone?: string;
  status?: string;
}

export type UpdateUserPayload = Partial<Omit<CreateUserPayload, 'password'>> & {
  password?: string;
};

export const userApi = {
  /** 登录 */
  login: (payload: LoginPayload) =>
    http.post<AuthResult>('/api/user/login', payload),

  /** 注册 */
  register: (payload: RegisterPayload) =>
    http.post<AuthResult>('/api/user/register', payload),

  /** 刷新令牌 */
  refreshToken: () =>
    http.post<{ token: string }>('/api/user/refresh'),

  /** 登出 */
  logout: () =>
    http.post<void>('/api/user/logout'),

  /** 获取当前用户信息 */
  getCurrentUser: () =>
    http.get<UserDTO>('/api/user/current'),

  /** 更新当前用户资料 */
  updateProfile: (payload: UpdateProfilePayload) =>
    http.put<UserDTO>('/api/user/profile', payload),

  /** 修改密码 */
  changePassword: (payload: ChangePasswordPayload) =>
    http.put<{ msg: string }>('/api/user/password', payload),

  /** 用户列表（分页+搜索） */
  list: (params: UserListParams = {}) =>
    http.get<Paginated<UserDTO>>('/api/user', params),

  /** 创建用户 */
  create: (payload: CreateUserPayload) =>
    http.post<UserDTO>('/api/user', payload),

  /** 获取用户详情 */
  getById: (id: number) =>
    http.get<UserDTO>(`/api/user/${id}`),

  /** 更新用户 */
  update: (id: number, payload: UpdateUserPayload) =>
    http.put<UserDTO>(`/api/user/${id}`, payload),

  /** 删除用户 */
  delete: (id: number) =>
    http.delete<void>(`/api/user/${id}`),

  /** 启用/禁用用户 */
  toggleStatus: (id: number) =>
    http.put<{ id: number; status: string; msg: string }>(`/api/user/${id}/status`),
};

export default userApi;
