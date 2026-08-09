<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { message } from 'antdv-next';
import type { UploadProps } from 'antdv-next';
import { useSystemUserStore, type SystemUser, type UserRole, type UserStatus } from '../../store/systemUser';
import { exportToCsv, rowsFromCsv, type CsvColumn } from '../../utils/csv';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
  FileExcelOutlined,
} from '@antdv-next/icons';

const store = useSystemUserStore();

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: SystemUser, b: SystemUser) => a.id - b.id },
  { title: '用户名', dataIndex: 'username', key: 'username', sorter: (a: SystemUser, b: SystemUser) => a.username.localeCompare(b.username) },
  { title: '姓名', dataIndex: 'name', key: 'name' },
  { title: '邮箱', dataIndex: 'email', key: 'email' },
  { title: '角色', dataIndex: 'role', key: 'role', width: 110 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 120 },
  { title: '操作', key: 'action', width: 150, fixed: 'right' },
];

const csvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'username', title: '用户名' },
  { key: 'name', title: '姓名' },
  { key: 'email', title: '邮箱' },
  { key: 'role', title: '角色' },
  { key: 'status', title: '状态' },
  { key: 'createdAt', title: '创建时间' },
];

const keyword = ref('');
const roleFilter = ref<UserRole | undefined>();
const statusFilter = ref<UserStatus | undefined>();
const pagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const filtered = computed(() =>
  store.list.filter((u) => {
    const kw = keyword.value.trim().toLowerCase();
    const matchKw =
      !kw ||
      u.username.toLowerCase().includes(kw) ||
      u.name.toLowerCase().includes(kw) ||
      u.email.toLowerCase().includes(kw);
    const matchRole = !roleFilter.value || u.role === roleFilter.value;
    const matchStatus = !statusFilter.value || u.status === statusFilter.value;
    return matchKw && matchRole && matchStatus;
  }),
);

const roles: UserRole[] = ['管理员', '编辑', '普通用户'];
const statuses: UserStatus[] = ['启用', '禁用'];

const modalVisible = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const formState = reactive<Omit<SystemUser, 'id' | 'createdAt'>>({
  username: '',
  name: '',
  email: '',
  role: '普通用户',
  status: '启用',
});

function resetForm() {
  Object.assign(formState, {
    username: '',
    name: '',
    email: '',
    role: '普通用户',
    status: '启用',
  });
}

function openCreate() {
  editingId.value = null;
  resetForm();
  modalVisible.value = true;
}

function openEdit(record: SystemUser) {
  editingId.value = record.id;
  Object.assign(formState, {
    username: record.username,
    name: record.name,
    email: record.email,
    role: record.role,
    status: record.status,
  });
  modalVisible.value = true;
}

function handleSubmit() {
  if (!formState.username.trim() || !formState.name.trim()) {
    message.warning('请填写用户名和姓名');
    return;
  }
  submitting.value = true;
  if (editingId.value) {
    store.update(editingId.value, { ...formState });
    message.success('更新成功');
  } else {
    store.add({ ...formState });
    message.success('创建成功');
  }
  submitting.value = false;
  modalVisible.value = false;
}

function handleDelete(id: number) {
  store.remove(id);
  message.success('删除成功');
}

function handleRefresh() {
  keyword.value = '';
  roleFilter.value = undefined;
  statusFilter.value = undefined;
  pagination.current = 1;
  message.success('已刷新');
}

function handleExport() {
  exportToCsv('用户数据.csv', csvColumns, store.list as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.list.length} 条数据`);
}

const beforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<SystemUser>(text, csvColumns).map((r) => ({
      username: r.username || '',
      name: r.name || '',
      email: r.email || '',
      role: (r.role as UserRole) || '普通用户',
      status: (r.status as UserStatus) || '启用',
    }));
    if (rows.length === 0) {
      message.warning('未解析到有效数据');
      return;
    }
    store.importRows(rows as SystemUser[]);
    message.success(`成功导入 ${rows.length} 条数据`);
  };
  reader.readAsText(file);
  return false;
};
</script>

<template>
  <div class="page">
    <a-card class="toolbar" variant="borderless">
      <a-form layout="inline" class="filter-form">
        <a-form-item label="关键词">
          <a-input
            v-model:value="keyword"
            placeholder="用户名 / 姓名 / 邮箱"
            allow-clear
            style="width: 220px"
          >
            <template #prefix><SearchOutlined /></template>
          </a-input>
        </a-form-item>
        <a-form-item label="角色">
          <a-select v-model:value="roleFilter" placeholder="全部" allow-clear style="width: 130px">
            <a-select-option v-for="r in roles" :key="r" :value="r">{{ r }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="statusFilter" placeholder="全部" allow-clear style="width: 110px">
            <a-select-option v-for="s in statuses" :key="s" :value="s">{{ s }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
      <div class="actions">
        <a-button type="primary" @click="openCreate"><PlusOutlined />新增</a-button>
        <a-upload :before-upload="beforeUpload" :show-upload-list="false" accept=".csv">
          <a-button><UploadOutlined />导入</a-button>
        </a-upload>
        <a-button @click="handleExport"><DownloadOutlined />导出</a-button>
        <a-button @click="handleRefresh"><ReloadOutlined />刷新</a-button>
        <a-button type="link" @click="handleExport"><FileExcelOutlined />下载模板</a-button>
      </div>
    </a-card>

    <a-card variant="borderless" class="table-card">
      <a-table
        :columns="columns"
        :data-source="filtered"
        :pagination="pagination"
        row-key="id"
        :scroll="{ x: 900 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'status'">
            <a-tag :color="record.status === '启用' ? 'green' : 'red'">{{ record.status }}</a-tag>
          </template>
          <template v-else-if="column.key === 'role'">
            <a-tag color="blue">{{ record.role }}</a-tag>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button type="link" size="small" @click="openEdit(record)">
                <EditOutlined />编辑
              </a-button>
              <a-popconfirm title="确认删除该用户？" @confirm="handleDelete(record.id)">
                <a-button type="link" size="small" danger>
                  <DeleteOutlined />删除
                </a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="modalVisible"
      :title="editingId ? '编辑用户' : '新增用户'"
      @ok="handleSubmit"
      :confirm-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="用户名" required>
          <a-input v-model:value="formState.username" placeholder="请输入用户名" />
        </a-form-item>
        <a-form-item label="姓名" required>
          <a-input v-model:value="formState.name" placeholder="请输入姓名" />
        </a-form-item>
        <a-form-item label="邮箱">
          <a-input v-model:value="formState.email" placeholder="请输入邮箱" />
        </a-form-item>
        <a-form-item label="角色">
          <a-select v-model:value="formState.role">
            <a-select-option v-for="r in roles" :key="r" :value="r">{{ r }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="formState.status">
            <a-select-option v-for="s in statuses" :key="s" :value="s">{{ s }}</a-select-option>
          </a-select>
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
