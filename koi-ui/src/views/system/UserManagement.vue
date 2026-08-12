<script setup lang="ts">
import { nextTick, reactive, ref } from 'vue';
import { App } from 'antdv-next';
const { message, modal } = App.useApp();
import type { FormInstance, Rule, UploadProps } from 'antdv-next';
import { useAuthStore } from '../../store/auth';
import userApi, { type UserDTO, type UserListParams, type CreateUserPayload, type UpdateUserPayload, type Paginated } from '../../services/userApi';
import { exportToCsv, rowsFromCsv, type CsvColumn } from '../../utils/csv';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
  StopOutlined,
  CheckCircleOutlined,
  KeyOutlined,
} from '@antdv-next/icons';

const auth = useAuthStore();

/* ---- 列表数据 ---- */
const listData = ref<Paginated<UserDTO> | null>(null);
const loading = ref(false);

/* ---- 筛选条件 ---- */
const keyword = ref('');
const statusFilter = ref<string>('all');

/** 搜索状态筛选选项（数据驱动，确保下拉有数据） */
const statusOptions = [
  { label: '全部', value: 'all' },
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'inactive' },
];
/** 表单内状态选项 */
const statusFormOptions = [
  { label: '启用', value: 'active' },
  { label: '禁用', value: 'inactive' },
];
const pagination = reactive({
  current: 1,
  pageSize: 10,
  showSizeChanger: true,
  showTotal: (total: number) => `共 ${total} 条`,
});

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
  { title: '用户名', dataIndex: 'username', key: 'username' },
  { title: '昵称', dataIndex: 'nickname', key: 'nickname' },
  { title: '邮箱', dataIndex: 'email', key: 'email' },
  { title: '手机号', dataIndex: 'phone', key: 'phone', width: 130 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 170 },
  { title: '操作', key: 'action', width: 340, fixed: 'right' },
];

const csvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'username', title: '用户名' },
  { key: 'nickname', title: '昵称' },
  { key: 'email', title: '邮箱' },
  { key: 'phone', title: '手机号' },
  { key: 'status', title: '状态' },
  { key: 'created_at', title: '创建时间' },
];

/** 当前表格数据 */
const dataSource = computed(() => listData.value?.items ?? []);

/* ---- 查询 ---- */
async function fetchList() {
  loading.value = true;
  try {
    const params: UserListParams = {
      page: pagination.current,
      pageSize: pagination.pageSize,
    };
    if (keyword.value.trim()) params.keyword = keyword.value.trim();
    if (statusFilter.value && statusFilter.value !== 'all') {
      params.status = statusFilter.value;
    }

    listData.value = await userApi.list(params);
    pagination.current = listData.value!.page;
    pagination.pageSize = listData.value!.pageSize;
  } catch (err) {
    message.error((err as Error).message || '加载用户列表失败');
  } finally {
    loading.value = false;
  }
}

function onSearch() {
  pagination.current = 1;
  fetchList();
}

function handleRefresh() {
  keyword.value = '';
  statusFilter.value = 'all';
  pagination.current = 1;
  fetchList();
}

function onPageChange(page: number, pageSize: number) {
  pagination.current = page;
  pagination.pageSize = pageSize;
  fetchList();
}

fetchList();

/* ---- 新增 / 编辑弹窗 ---- */
const modalVisible = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const formRef = ref<FormInstance>();

const formState = reactive<{
  username: string;
  password: string;
  nickname: string;
  email: string;
  phone: string;
  status: string;
}>({
  username: '',
  password: '',
  nickname: '',
  email: '',
  phone: '',
  status: 'active',
});

/** 校验规则：编辑模式下密码选填，新建必填 */
const formRules = computed<Record<string, Rule[]>>(() => ({
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' as const },
    { min: 5, message: '用户名至少 5 位', trigger: 'blur' as const },
  ],
  password: editingId.value
    ? [{ min: 6, message: '密码至少 6 位', trigger: 'blur' as const }]
    : [
        { required: true, message: '请输入密码', trigger: 'blur' as const },
        { min: 6, message: '密码至少 6 位', trigger: 'blur' as const },
      ],
  email: [{ type: 'email', message: '邮箱格式不正确', trigger: 'blur' as const }],
  phone: [{ pattern: /^1[3-9]\d{9}$/, message: '手机号格式不正确', trigger: 'blur' as const }],
}));

function resetForm() {
  Object.assign(formState, {
    username: '',
    password: '',
    nickname: '',
    email: '',
    phone: '',
    status: 'active',
  });
}

function openCreate() {
  editingId.value = null;
  resetForm();
  modalVisible.value = true;
  nextTick(() => formRef.value?.clearValidate());
}

function openEdit(record: UserDTO) {
  editingId.value = record.id;
  Object.assign(formState, {
    username: record.username,
    password: '', // 编辑时不回填密码
    nickname: record.nickname,
    email: record.email,
    phone: record.phone,
    status: record.status,
  });
  modalVisible.value = true;
  nextTick(() => formRef.value?.clearValidate());
}

/** Modal 确认：先校验再提交，失败时保持弹窗打开 */
async function onModalOk() {
  if (!formRef.value) return;
  await formRef.value.validate();
  submitting.value = true;
  try {
    if (editingId.value) {
      const payload: UpdateUserPayload = {
        nickname: formState.nickname || undefined,
        email: formState.email || undefined,
        phone: formState.phone || undefined,
        status: formState.status || undefined,
      };
      if (formState.password) payload.password = formState.password;
      await userApi.update(editingId.value, payload);
      message.success('更新成功');
    } else {
      const payload: CreateUserPayload = {
        username: formState.username.trim(),
        password: formState.password,
        nickname: formState.nickname || undefined,
        email: formState.email || undefined,
        phone: formState.phone || undefined,
        status: formState.status || 'active',
      };
      await userApi.create(payload);
      message.success('创建成功');
    }
    modalVisible.value = false;
    fetchList();
  } catch (err) {
    message.error((err as Error).message || '操作失败');
    return Promise.reject(err);
  } finally {
    submitting.value = false;
  }
}

/* ---- 删除 ---- */
async function handleDelete(id: number) {
  try {
    await userApi.delete(id);
    message.success('删除成功');
    fetchList();
  } catch (err) {
    message.error((err as Error).message || '删除失败');
  }
}

/** 删除二次确认（防止误删，禁止删除自己） */
function confirmDelete(record: UserDTO) {
  if (auth.user?.id === record.id) {
    message.warning('不能删除自己');
    return;
  }
  modal.confirm({
    title: '确认删除该用户？',
    content: `用户名：${record.username}`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await handleDelete(record.id);
    },
  });
}

/* ---- 启用 / 禁用 ---- */
async function handleToggleStatus(record: UserDTO) {
  if (auth.user && auth.user.id === record.id) {
    message.warning('不能修改自己的状态');
    return;
  }
  try {
    const result = await userApi.toggleStatus(record.id);
    message.success(result.msg || '操作成功');
    fetchList();
  } catch (err) {
    message.error((err as Error).message || '操作失败');
  }
}

/* ---- 重置密码弹窗 ---- */
const pwdModalVisible = ref(false);
const pwdUserId = ref<number | null>(null);
const pwdUsername = ref('');
const pwdSubmitting = ref(false);
const pwdFormRef = ref<FormInstance>();
const pwdForm = reactive({ password: '' });
const pwdRules: Record<string, Rule[]> = {
  password: [
    { required: true, message: '请输入新密码', trigger: 'blur' as const },
    { min: 6, message: '密码至少 6 位', trigger: 'blur' as const },
  ],
};

function openResetPwd(record: UserDTO) {
  pwdUserId.value = record.id;
  pwdUsername.value = record.username;
  pwdForm.password = '';
  pwdModalVisible.value = true;
  nextTick(() => pwdFormRef.value?.clearValidate());
}

async function onPwdModalOk() {
  if (!pwdFormRef.value || pwdUserId.value == null) return;
  await pwdFormRef.value.validate();
  pwdSubmitting.value = true;
  try {
    await userApi.update(pwdUserId.value, { password: pwdForm.password });
    message.success('密码重置成功');
    pwdModalVisible.value = false;
  } catch (err) {
    message.error((err as Error).message || '重置失败');
    return Promise.reject(err);
  } finally {
    pwdSubmitting.value = false;
  }
}

/* ---- 导出 ---- */
function handleExport() {
  const items = dataSource.value;
  if (items.length === 0) {
    message.warning('没有可导出的数据');
    return;
  }
  exportToCsv('用户数据.csv', csvColumns, items as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${items.length} 条数据`);
}

/* ---- 导入 ---- */
const importLoading = ref(false);
const beforeUpload: UploadProps['beforeUpload'] = async (file) => {
  importLoading.value = true;
  try {
    const text = await file.text();
    const rows = rowsFromCsv<Record<string, string>>(text, csvColumns);
    if (rows.length === 0) {
      message.warning('未解析到有效数据');
      return false;
    }

    let successCount = 0;
    let failCount = 0;
    for (const row of rows) {
      if (!row.username || !row.password) {
        failCount++;
        continue;
      }
      try {
        await userApi.create({
          username: String(row.username).trim(),
          password: String(row.password || '123456').trim(),
          nickname: row.nickname ? String(row.nickname).trim() : undefined,
          email: row.email ? String(row.email).trim() : undefined,
          phone: row.phone ? String(row.phone).trim() : undefined,
          status: row.status === 'inactive' ? 'inactive' : 'active',
        });
        successCount++;
      } catch {
        failCount++;
      }
    }
    message.success(`导入完成：成功 ${successCount} 条，失败 ${failCount} 条`);
    fetchList();
  } catch {
    message.error('文件读取失败');
  } finally {
    importLoading.value = false;
  }
  return false;
};
</script>

<template>
  <div class="page">
    <!-- 顶部工具栏 -->
    <a-card class="toolbar" variant="borderless">
      <a-form layout="inline" class="filter-form">
        <a-form-item label="关键词">
          <a-input
            v-model:value="keyword"
            placeholder="用户名 / 昵称 / 邮箱"
            allow-clear
            style="width: 220px"
            @press-enter="onSearch"
          >
            <template #prefix><SearchOutlined /></template>
          </a-input>
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="statusFilter" :options="statusOptions" placeholder="全部" allow-clear style="width: 120px" @change="onSearch" />
        </a-form-item>
        <a-form-item>
          <a-button type="primary" @click="onSearch">
            <SearchOutlined />查询
          </a-button>
        </a-form-item>
        <a-form-item>
          <a-button @click="handleRefresh">
            <ReloadOutlined />重置
          </a-button>
        </a-form-item>
      </a-form>

      <div class="actions">
        <a-button type="primary" @click="openCreate"><PlusOutlined />新增</a-button>
        <a-upload :before-upload="beforeUpload" :show-upload-list="false" accept=".csv">
          <a-button :loading="importLoading"><UploadOutlined />导入</a-button>
        </a-upload>
        <a-button @click="handleExport"><DownloadOutlined />导出</a-button>
        <a-button @click="handleRefresh"><ReloadOutlined />刷新</a-button>
      </div>
    </a-card>

    <!-- 表格 -->
    <a-card variant="borderless" class="table-card">
      <a-table
        :columns="columns"
        :data-source="dataSource"
        :pagination="{ ...pagination, onChange: onPageChange, onShowSizeChange: onPageChange, total: listData?.total ?? 0 }"
        :loading="loading"
        row-key="id"
        :scroll="{ x: 'max-content' }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'status'">
            <a-tag :color="record.status === 'active' ? 'green' : 'red'">
              {{ record.status === 'active' ? '启用' : '禁用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space size="small">
              <a-button type="link" size="small" @click="openEdit(record)">
                <EditOutlined />编辑
              </a-button>
              <a-button type="link" size="small" :disabled="auth.user?.id === record.id" @click="handleToggleStatus(record)">
                <template v-if="record.status === 'active'"><StopOutlined />禁用</template>
                <template v-else><CheckCircleOutlined />启用</template>
              </a-button>
              <a-button type="link" size="small" :disabled="auth.user?.id === record.id" @click="openResetPwd(record)">
                <KeyOutlined />重置密码
              </a-button>
              <a-button type="link" size="small" danger :disabled="auth.user?.id === record.id" @click="confirmDelete(record)">
                <DeleteOutlined />删除
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <!-- 新增 / 编辑弹窗 -->
    <a-modal
      v-model:open="modalVisible"
      :title="editingId ? '编辑用户' : '新增用户'"
      @ok="onModalOk"
      :confirm-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form ref="formRef" :model="formState" :rules="formRules" :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="用户名" name="username" required>
          <a-input v-model:value="formState.username" :disabled="!!editingId" placeholder="请输入用户名（至少 5 位）" />
        </a-form-item>
        <a-form-item label="密码" name="password" :required="!editingId">
          <a-input-password
            v-model:value="formState.password"
            :placeholder="editingId ? '留空则不修改密码' : '请输入密码（至少 6 位）'"
          />
        </a-form-item>
        <a-form-item label="昵称" name="nickname">
          <a-input v-model:value="formState.nickname" placeholder="请输入昵称" />
        </a-form-item>
        <a-form-item label="邮箱" name="email">
          <a-input v-model:value="formState.email" placeholder="请输入邮箱" />
        </a-form-item>
        <a-form-item label="手机号" name="phone">
          <a-input v-model:value="formState.phone" placeholder="请输入手机号" />
        </a-form-item>
        <a-form-item label="状态" name="status">
          <a-select v-model:value="formState.status" :options="statusFormOptions" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 重置密码弹窗 -->
    <a-modal
      v-model:open="pwdModalVisible"
      :title="`重置密码 - ${pwdUsername}`"
      @ok="onPwdModalOk"
      :confirm-loading="pwdSubmitting"
      ok-text="确定"
      cancel-text="取消"
    >
      <a-form ref="pwdFormRef" :model="pwdForm" :rules="pwdRules" :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="新密码" name="password" required>
          <a-input-password v-model:value="pwdForm.password" placeholder="请输入新密码（至少 6 位）" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.toolbar {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 12px 16px;
}
.toolbar :deep(.ant-form-item-label > label) {
  color: var(--color-text-secondary);
}
.filter-form {
  margin-bottom: 12px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.table-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.modal-form {
  margin-top: 16px;
}
.modal-form :deep(.ant-form-item-label > label) {
  color: var(--color-text);
}
</style>
