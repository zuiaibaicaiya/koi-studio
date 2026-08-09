# 热词库管理接口文档

## 概述

热词库管理提供热词库与热词的完整增删改查能力。每个**热词库**包含多个**热词**，二者为一对多关系：

- 热词库：`id`、`name`、`description`、`status`、`wordCount`
- 热词：`id`、`libraryId`、`word`、`weight`

记录均采用软删除；删除热词库时会级联软删除其下所有热词。

### 基础信息

| 项目 | 说明 |
| --- | --- |
| Base URL | `http://<host>:<port>/api` |
| 字符编码 | UTF-8 |
| 请求/响应格式 | `application/json`（文件上传除外） |
| 鉴权方式 | JWT，需在请求头携带 `Authorization: Bearer <token>` |

> 所有热词库 / 热词接口均挂载在 JWT 中间件下，未携带有效令牌将返回 401。

### 统一响应格式

**成功（普通）**

```json
{
  "code": 0,
  "msg": "success",
  "data": { }
}
```

**成功（分页列表）**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [ ],
    "total": 100,
    "page": 1,
    "pageSize": 16,
    "totalPage": 7
  }
}
```

**失败**

```json
{
  "code": 1,
  "msg": "错误信息",
  "timestamp": 1723000000,
  "request_id": "xxxxx"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `code` | int | `0` 成功，非 `0` 失败 |
| `msg` | string | 提示信息 |
| `data` | object/array | 业务数据；分页接口下为包含 `items`/`total`/`page`/`pageSize`/`totalPage` 的对象 |
| `total` | int | 总记录数 |
| `totalPage` | int | 总页数 |
| `page` | int | 当前页码（默认 1） |
| `pageSize` | int | 每页数量（默认 16，最大 100） |

---

## 一、热词库接口

### 1.1 热词库列表

获取热词库分页列表，支持关键词与状态筛选。按创建时间倒序返回。

- **请求**：`GET /hot-word-library`
- **鉴权**：需要

**Query 参数**

| 参数 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `page` | int | 否 | 页码，默认 1 | `min:1` |
| `pageSize` | int | 否 | 每页数量，默认 16 | `min:1`，最大 100 |
| `keyword` | string | 否 | 名称模糊搜索 | — |
| `status` | string | 否 | 状态筛选 | `in:active,inactive` |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 1,
        "name": "通用行业热词",
        "description": "用于语音识别的通用行业热词",
        "status": "active",
        "wordCount": 120,
        "created_at": "2026-08-09T10:00:00Z",
        "updated_at": "2026-08-09T10:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 16,
    "totalPage": 1
  }
}
```

---

### 1.2 创建热词库

手动创建一个空热词库（不含热词），后续通过热词接口向其添加内容。

- **请求**：`POST /hot-word-library`
- **鉴权**：需要
- **Content-Type**：`application/json`

**请求体**

| 字段 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `name` | string | 是 | 热词库名称，全局唯一 | `required`，`max_len:100` |
| `description` | string | 否 | 描述 | `max_len:255` |
| `status` | string | 否 | 状态，默认 `active` | `in:active,inactive` |

**请求示例**

```json
{
  "name": "通用行业热词",
  "description": "用于语音识别的通用行业热词",
  "status": "active"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "通用行业热词",
    "description": "用于语音识别的通用行业热词",
    "status": "active",
    "wordCount": 0
  }
}
```

**错误码说明**

- 名称已存在：`{"code":1,"msg":"热词库已经存在"}`

---

### 1.3 导入 Excel 创建热词库

上传 Excel 文件，**以文件名（去除扩展名）作为热词库名称**，并解析其中热词批量入库。

- **请求**：`POST /hot-word-library/import`
- **鉴权**：需要
- **Content-Type**：`multipart/form-data`

**表单字段**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | File | 是 | Excel 文件，支持 `xlsx`/`xlsm`/`xltx`/`xltm` |
| `description` | string | 否 | 热词库描述 |

**Excel 规范**

- 第一列：热词内容（`热词`/`词语`/`word`/`hotword` 等表头行会被自动跳过）
- 第二列：热词权重（整数，缺省为 0）
- 文件内重复的热词会自动去重

**请求示例**

```bash
curl -X POST http://<host>:<port>/api/hot-word-library/import \
  -H "Authorization: Bearer <token>" \
  -F "file=@通用行业热词.xlsx" \
  -F "description=导入自Excel"
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 2,
    "name": "通用行业热词",
    "description": "导入自Excel",
    "status": "active",
    "wordCount": 30
  }
}
```

**错误码说明**

- 未选择文件：`{"code":1,"msg":"请选择需要导入的Excel文件"}`
- 格式不支持：`{"code":1,"msg":"仅支持xlsx格式的Excel文件"}`
- 名称已存在：`{"code":1,"msg":"热词库已经存在"}`
- 解析失败：返回具体解析错误信息

> 库与热词在单个数据库事务中创建；库名与已有库重名时直接拒绝导入。

---

### 1.4 热词库详情

- **请求**：`GET /hot-word-library/{id}`
- **鉴权**：需要

**路径参数**

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 热词库 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "通用行业热词",
    "description": "用于语音识别的通用行业热词",
    "status": "active",
    "wordCount": 120
  }
}
```

**错误码说明**

- ID 非法：`{"code":1,"msg":"热词库ID不正确"}`
- 不存在：`{"code":1,"msg":"热词库不存在"}`

---

### 1.5 更新热词库

仅更新传入的字段（部分更新）。

- **请求**：`PUT /hot-word-library/{id}`
- **鉴权**：需要
- **Content-Type**：`application/json`

**路径参数**：`id`（热词库 ID）

**请求体**

| 字段 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `name` | string | 否 | 热词库名称 | `max_len:100`，更新时需保证全局唯一 |
| `description` | string | 否 | 描述 | `max_len:255` |
| `status` | string | 否 | 状态 | `in:active,inactive` |

**请求示例**

```json
{
  "description": "更新后的描述",
  "status": "inactive"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "通用行业热词",
    "description": "更新后的描述",
    "status": "inactive",
    "wordCount": 120
  }
}
```

**错误码说明**

- 名称已存在：`{"code":1,"msg":"热词库已经存在"}`

---

### 1.6 删除热词库

软删除热词库，并级联软删除其下所有热词。

- **请求**：`DELETE /hot-word-library/{id}`
- **鉴权**：需要

**路径参数**：`id`（热词库 ID）

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": { }
}
```

**错误码说明**

- 不存在：`{"code":1,"msg":"热词库不存在"}`

---

## 二、热词接口

> 所有热词接口的路径前缀均为 `/hot-word-library/{id}/word`，其中 `{id}` 为所属热词库 ID。接口会校验热词库存在性及其归属关系。

### 2.1 热词列表

获取指定热词库下的热词分页列表，按权重倒序、ID 倒序返回。

- **请求**：`GET /hot-word-library/{id}/word`
- **鉴权**：需要

**路径参数**：`id`（热词库 ID）

**Query 参数**

| 参数 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `page` | int | 否 | 页码，默认 1 | `min:1` |
| `pageSize` | int | 否 | 每页数量，默认 16 | `min:1`，最大 100 |
| `keyword` | string | 否 | 热词内容模糊搜索 | — |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": 5,
        "libraryId": 1,
        "word": "人工智能",
        "weight": 10,
        "created_at": "2026-08-09T10:00:00Z",
        "updated_at": "2026-08-09T10:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 16,
    "totalPage": 1
  }
}
```

---

### 2.2 添加热词

向指定热词库新增一个热词。

- **请求**：`POST /hot-word-library/{id}/word`
- **鉴权**：需要
- **Content-Type**：`application/json`

**路径参数**：`id`（热词库 ID）

**请求体**

| 字段 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `word` | string | 是 | 热词内容，库内唯一 | `required`，`max_len:100` |
| `weight` | int | 否 | 权重，默认 0 | `int`，`min:0`，`max:10000` |

**请求示例**

```json
{
  "word": "人工智能",
  "weight": 10
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 5,
    "libraryId": 1,
    "word": "人工智能",
    "weight": 10
  }
}
```

**错误码说明**

- 已存在：`{"code":1,"msg":"该热词已经存在"}`

---

### 2.3 热词详情

- **请求**：`GET /hot-word-library/{id}/word/{wordId}`
- **鉴权**：需要

**路径参数**

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int | 热词库 ID |
| `wordId` | int | 热词 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 5,
    "libraryId": 1,
    "word": "人工智能",
    "weight": 10
  }
}
```

**错误码说明**

- 热词不存在：`{"code":1,"msg":"热词不存在"}`
- 不属于该库：`{"code":1,"msg":"热词不属于该热词库"}`

---

### 2.4 更新热词

仅更新传入的字段（部分更新）。权重使用指针语义，传入 `0` 即表示将权重更新为 0，不传则不修改权重。

- **请求**：`PUT /hot-word-library/{id}/word/{wordId}`
- **鉴权**：需要
- **Content-Type**：`application/json`

**路径参数**：`id`（热词库 ID）、`wordId`（热词 ID）

**请求体**

| 字段 | 类型 | 必填 | 说明 | 校验规则 |
| --- | --- | --- | --- | --- |
| `word` | string | 否 | 热词内容，库内唯一 | `max_len:100`，更新时需保证库内唯一 |
| `weight` | int | 否 | 权重；不传则不修改 | `int`，`min:0`，`max:10000` |

**请求示例**（将权重改为 0，热词内容不变）

```json
{
  "weight": 0
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 5,
    "libraryId": 1,
    "word": "人工智能",
    "weight": 0
  }
}
```

**错误码说明**

- 已存在：`{"code":1,"msg":"该热词已经存在"}`

---

### 2.5 删除热词

软删除指定热词，并更新所属热词库的 `wordCount`。

- **请求**：`DELETE /hot-word-library/{id}/word/{wordId}`
- **鉴权**：需要

**路径参数**：`id`（热词库 ID）、`wordId`（热词 ID）

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": { }
}
```

**错误码说明**

- 热词不存在：`{"code":1,"msg":"热词不存在"}`
- 不属于该库：`{"code":1,"msg":"热词不属于该热词库"}`

---

## 三、错误码汇总

| code | msg | 触发场景 |
| --- | --- | --- |
| 0 | `success` | 成功 |
| 1 | `热词库ID不正确` | 路径参数 `id` 非法 |
| 1 | `热词库不存在` | 热词库未找到 |
| 1 | `热词库已经存在` | 创建/更新/导入时名称冲突 |
| 1 | `请选择需要导入的Excel文件` | 导入未上传文件 |
| 1 | `仅支持xlsx格式的Excel文件` | 导入文件格式不支持 |
| 1 | `热词ID不正确` | 路径参数 `wordId` 非法 |
| 1 | `热词不存在` | 热词未找到 |
| 1 | `热词不属于该热词库` | 热词与热词库归属校验失败 |
| 1 | `该热词已经存在` | 创建/更新热词时内容冲突 |
| 1 | 校验失败信息 | 请求参数未通过校验规则 |

---

## 四、调用示例

```bash
TOKEN="<your-jwt-token>"

# 1. 导入 Excel 创建热词库（文件名作为库名）
curl -X POST http://<host>:<port>/api/hot-word-library/import \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@通用行业热词.xlsx"

# 2. 查询热词库列表
curl "http://<host>:<port>/api/hot-word-library?page=1&pageSize=10" \
  -H "Authorization: Bearer $TOKEN"

# 3. 向热词库添加热词
curl -X POST http://<host>:<port>/api/hot-word-library/1/word \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"word":"深度学习","weight":8}'

# 4. 分页查询热词
curl "http://<host>:<port>/api/hot-word-library/1/word?page=1&pageSize=20" \
  -H "Authorization: Bearer $TOKEN"
```
