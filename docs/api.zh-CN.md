# FeedMeDaily 本地 API 文档

本文记录 FeedMeDaily 当前本地 HTTP API。服务由 `cmd/feedmedailyd` 提供，路由注册在 `internal/api/server.go`，前端调用集中在 `web/src/api/client.ts`。

## 1. 通用约定

- Base URL：source mode 默认 `http://127.0.0.1:8000`。
- Content-Type：请求和响应默认使用 JSON。
- 错误格式：

```json
{"detail":"Method not allowed."}
```

- 时间：响应中的时间通常是 RFC3339 字符串。
- CORS：本地服务对前端放开 CORS。
- Job：长任务返回 `{"job": JobInfo}`，前端通过 `/api/admin/jobs/{id}` 轮询。
- 静态资源：非 `/api/` 的 GET/HEAD 请求由静态前端或 SPA fallback 处理。

核心 JobInfo 字段：

```ts
{
  id: string;
  job_type: string;
  status: "queued" | "running" | "waiting_for_user" | "completed" | "failed" | "cancelled";
  cancel_requested?: boolean;
  message_key?: string;
  message?: string;
  error?: string;
  progress_stage?: string;
  progress_current?: number;
  progress_total?: number;
  progress_percent?: number;
  verification_required?: boolean;
  verification_feed_url?: string;
  verification_method?: "native_webview2" | "browser_manual";
  result?: Record<string, unknown>;
  log_path?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}
```

## 2. App

### `GET /api/app/health`

用途：轻量探活，供托盘和外部调用方确认后端可用。

请求体：无。

成功响应：

```json
{
  "status": "ok",
  "name": "FeedMeDaily",
  "version": "0.3.3",
  "mode": "source",
  "server_url": "http://127.0.0.1:8000"
}
```

常见错误：非 GET 返回 405。

前端入口：当前主要由托盘或外部探活使用，`web/src/api/client.ts` 未封装此接口。

### `GET /api/app/meta`

用途：读取运行路径、版本、模式、调度任务名和进程状态。

请求体：无。

成功响应字段：

- `name`
- `version`
- `mode`
- `install_dir`
- `config_dir`
- `data_dir`
- `logs_dir`
- `static_dir`
- `tray_settings_path`
- `tray_instance_id`
- `server_url`
- `scheduler_task_name`
- `process_running`

常见错误：非 GET 返回 405。

前端入口：`fetchAppMeta()`。

### `GET /api/app/update`

用途：读取 DNS TXT update manifest，判断是否有新版本。

Query：

- `force=1`：绕过普通缓存，手动刷新。

成功响应：

```json
{
  "status": "up_to_date",
  "current_version": "0.3.3",
  "latest_version": "0.3.3",
  "has_update": false,
  "download_url": "https://github.com/...",
  "release_notes_url": "https://github.com/...",
  "detail": null,
  "checked_at": "2026-06-23T00:00:00Z"
}
```

`status` 可能为 `checking`、`up_to_date`、`update_available`、`check_failed`。

常见错误：DNS 查询失败时通常仍返回 200，但 `status` 为 `check_failed`，`detail` 带错误。

前端入口：`fetchAppUpdate(force)`。

### `POST /api/app/open`

用途：打开本地目录、服务地址、下载地址或 release notes。

请求体：

```json
{"target":"logs_dir"}
```

`target` 可选：

- `data_dir`
- `logs_dir`
- `install_dir`
- `server_url`
- `download_url`
- `release_notes_url`

成功响应：

```json
{
  "ok": true,
  "action": "open",
  "target": "logs_dir",
  "detail": "D:\\Codes\\Projects\\SciRSSAgent\\logs"
}
```

常见错误：

- 400：不支持的 target。
- 500：系统打开失败。

前端入口：`openAppTarget(target)`。

### `POST /api/app/exit`

用途：请求本地服务优雅退出。

请求体：无。

成功响应：

```json
{
  "ok": true,
  "action": "exit",
  "detail": "Shutting down the local FeedMeDaily service."
}
```

常见错误：非 POST 返回 405。

前端入口：`exitApp()`。

## 3. Report and Papers

### `GET /api/report/latest`

用途：从 SQLite 实时组装最新论文报告，是 UI 首屏性能关键路径。

请求体：无。

成功响应：

```json
{
  "generated_at": "2026-06-23T00:00:00Z",
  "last_updated_at": "2026-06-23T00:00:00Z",
  "report_date": "2026-06-23",
  "totals": {"total": 0, "direct": 0, "indirect": 0, "unrelated": 0},
  "papers": [],
  "errors": []
}
```

`papers[]` 主要字段：

- `id`
- `title`
- `url`
- `doi`
- `journal`
- `authors`
- `abstract`
- `abstract_html`
- `abstract_images`
- `published_date`
- `seen_date`
- `read_at`
- `classification`
- `feedback_status`
- `zotero_status`

常见错误：

- 数据库不存在时返回结构完整的空报告。
- SQLite 读取失败返回 500。

前端入口：`fetchLatestReport()`。

### `GET /api/papers/{id}/abstract-image`

用途：代理当前论文摘要 HTML 中登记过的图片。部分 publisher 图片需要文章页 `Referer` 或浏览器 UA，前端不能直接伪造这些 header，因此由本地后端代取。

路径参数：

- `id`：paper id。

Query：

- `src`：必须精确匹配该 paper 的 `abstract_images[].src`。

成功响应：图片二进制，`Content-Type` 必须是 `image/*`。

常见错误：

- 400：`src` 缺失、不是 http/https 远程图片，或指向 localhost/private IP。
- 404：paper 不存在，或 `src` 不属于该 paper 的摘要图片。
- 502：publisher 图片请求失败，或返回的不是图片内容。

前端入口：右侧详情面板渲染 `abstract_html` 时，会把匹配到的 `<img src>` 改写到此接口。

### `POST /api/papers/{id}/read`

用途：把论文标记为已读。

路径参数：

- `id`：paper id。

请求体：无。

成功响应：

```json
{"paper_id":1,"read_at":"2026-06-23T00:00:00Z"}
```

常见错误：

- 404：论文不存在或路径不符合 `/api/papers/{id}/read`。
- 500：SQLite 写入失败。

前端入口：`markPaperRead(paperId)`。

## 4. Settings

### `GET /api/settings/config`

用途：读取可编辑配置字段、字段来源，以及固定分类模型目录的启用状态、默认模型和密钥元数据。

请求体：无。

成功响应：

```json
{
  "fields": [
    {
      "key": "SCIRSS_CLASSIFIER_MODEL",
      "label": "Classifier model",
      "description": "Model name used for paper classification.",
      "section": "Legacy classifier model",
      "input_type": "text",
      "secret": false,
      "configured": true,
      "source": "settings",
      "stored_in_dotenv": false,
      "storage_label": "Local settings",
      "value": "deepseek-v4-flash",
      "default_value": "deepseek-v4-flash",
      "options": []
    }
  ],
  "classifier_models": {
    "models": [
      {
        "id": "deepseek-v4-flash",
        "provider": "deepseek",
        "label": "DeepSeek V4 Flash",
        "base_url": "https://api.deepseek.com",
        "thinking": "disabled",
        "enabled": true,
        "default": true,
        "configured": true,
        "source": "dotenv",
        "stored_locally": true,
        "environment_override": false
      },
      {
        "id": "glm-5.3-flash",
        "provider": "zhipu",
        "label": "GLM-5.3-Flash",
        "base_url": "https://open.bigmodel.cn/api/paas/v4",
        "thinking": "enabled",
        "reasoning_effort": "low",
        "enabled": false,
        "default": false,
        "configured": false,
        "source": "unset",
        "stored_locally": false,
        "environment_override": false
      }
    ],
    "enabled_model_ids": ["deepseek-v4-flash"],
    "default_model_id": "deepseek-v4-flash"
  }
}
```

secret 字段和 `classifier_models` 中的 key 均不以明文返回。`source=environment` 且 `environment_override=true` 表示系统环境变量覆盖了本地值。

当前固定分类目录为 DeepSeek V4 Flash、Zhipu GLM-5.3-Flash、Qwen3.8-Flash 和 MiMo-V2.5。`SCIRSS_CLASSIFIER_THINKING` 只控制最低思考档：GLM 始终为 low；DeepSeek/Qwen 开启时为 low；MiMo 开启时为 enabled。DeepSeek/MiMo 开启时使用至少 4096 completion tokens。分类器默认 batch size 为 `5`。模型响应不要求 `decision_trace` 或 `recommended_action`；报告 API 中保留的 `recommended_action` 由后端按 relevance 确定。

`DeepSeek pricing` section 提供 Flash/Pro 各自的 off-peak/peak 价格；`GLM pricing` 提供 GLM-5.3-Flash 的缓存命中、普通输入和输出价格。单位均为 CNY / 1M tokens，价格允许 0 或最多三位小数。GLM 默认值为 2026-08-28 官方页面展示的限时 5 折价：`0.115 / 0.4 / 1.4`，促销结束后可在 UI 或 `SCIRSS_GLM_53_FLASH_<CACHE_HIT|CACHE_MISS|OUTPUT>_CNY_PER_MILLION` 更新。

前端入口：`fetchSettingsConfig()`。

### `PUT /api/settings/config`

用途：更新本地配置，并立即重载后端 settings。除传统 `fields` 外，可用 `classifier_models` 一次更新启用模型、默认模型和按模型密钥。定价修改只会被之后启动的 job 捕获；正在运行和已经完成的 job 保留启动时或完成时的价格快照。

请求体：

```json
{
  "fields": {
    "SCIRSS_ZOTERO_API_KEY": {"clear": true}
  },
  "classifier_models": {
    "enabled_model_ids": ["deepseek-v4-flash", "glm-5.3-flash"],
    "default_model_id": "deepseek-v4-flash",
    "credentials": {
      "deepseek-v4-flash": {"value": "new-deepseek-key"},
      "glm-5.3-flash": {"clear": true}
    },
    "reuse_deepseek_key_for_profile": false
  }
}
```

`credentials` 只需要提交新 key 或 `{ "clear": true }`；空对象表示保留已有 key。停用模型不会删除 key。至少启用一个固定模型，且默认模型必须属于启用集合；后端会拒绝未知模型、缺少默认模型约束或无效密钥更新。旧的 `SCIRSS_CLASSIFIER_API_KEY/BASE_URL/MODEL/THINKING` 会在首次结构化保存时迁移到对应 provider key，无法覆盖的系统环境变量仍优先并在响应中提示。

成功响应：同 `GET /api/settings/config`。

常见错误：

- 400：无效 JSON、未知字段、非法值。
- 500：更新后 settings 重载失败。

前端入口：`saveSettingsConfig(fields, classifierModels?)`。

### `POST /api/settings/classifier-models/test`

用途：以后台 `model-test` job 发送一个最小真实 JSON 请求，验证固定 endpoint、鉴权、模型参数和响应解析。连接测试不改变默认模型，不保存请求中的临时 key，并记录返回的 token 用量；请求会消耗少量对应提供商额度。

请求体：

```json
{"model_id":"glm-5.3-flash","api_key":"optional-unsaved-key"}
```

省略 `api_key` 时使用已保存 key。成功响应为 `{ "job": JobInfo }`，前端可通过 `GET /api/admin/jobs` 轮询；未知模型或没有可用 key 返回 400。日志不会写入 API key。

### `GET /api/settings/feeds`

用途：读取 RSS 订阅列表。

成功响应：

```json
[
  {"journal":"Nature Biomedical Engineering","url":"https://example.com/rss"}
]
```

文件不存在时返回空数组。

前端入口：`fetchFeedSubscriptions()`。

### `PUT /api/settings/feeds`

用途：校验、去重、标准化并保存 RSS 订阅。

请求体：

```json
{
  "feeds": [
    {"journal":"Nature Biomedical Engineering","url":"https://example.com/rss"}
  ]
}
```

成功响应：保存后的订阅数组。

常见错误：

- 400：`journal` 为空、URL 非 http/https 或 JSON 无效。
- 500：写文件失败。

前端入口：`saveFeedSubscriptions(feeds)`。

### `GET /api/settings/scheduler`

用途：读取本地日常同步设置。

成功响应字段：

- `installed`
- `task_name`
- `mode`
- `scheduler_backend`
- `scheduled_time`
- `settings_path`
- `tray_instance_id`
- `platform`
- `automatic_supported`
- `advisory`
- `command`

Linux source mode 只保存设置，不由 Web UI 自动执行；响应会给出 helper 命令。

没有本地 scheduler 设置时，`scheduled_time` 默认返回本地时间 `12:30`；该时间在中国标准时间下位于 DeepSeek 午间空闲窗口。已经保存的时间不会因默认值升级而改变。

前端入口：`fetchSchedulerSettings()`。

### `PUT /api/settings/scheduler`

用途：启用 daily sync 设置。

请求体：

```json
{"daily_time":"08:15"}
```

成功响应：scheduler settings。

常见错误：

- 400：`daily_time` 不是 `HH:MM`。

前端入口：`saveSchedulerSettings(dailyTime)`。

### `DELETE /api/settings/scheduler`

用途：禁用 daily sync 设置。

请求体：无。

成功响应：scheduler settings。

前端入口：`deleteSchedulerSettings()`。

## 5. Profile

### `GET /api/profile/current`

用途：读取当前分类 Profile。

成功响应：

```json
{"profile": null}
```

或：

```json
{"profile":{"meta":{"name":"Default","version":1},"scope":"..."}}
```

前端入口：`fetchCurrentProfile()`。

### `PUT /api/profile/current`

用途：保存当前 Profile。只有已存在 Profile 时可用。

请求体：完整 `ClassificationProfile`。

成功响应：

```json
{"profile": {"meta": {"version": 2}}}
```

常见错误：

- 400：无 Profile、JSON 无效或 Profile 校验失败。
- 500：读取或写入失败。

前端入口：`saveCurrentProfile(profile)`。

### `POST /api/profile/bootstrap`

用途：首次使用时，根据兴趣描述启动初始 Profile proposal job。

请求体：

```json
{
  "interest_description": "关注核酸化学和酶工程",
  "name": "My Profile"
}
```

成功响应：

```json
{"job": {"id":"...","job_type":"profile-bootstrap","status":"queued"}}
```

常见错误：

- 400：已有 Profile、兴趣描述为空或 JSON 无效。
- 500：读取当前 Profile 失败。

前端入口：`bootstrapProfile(input)`。

### `GET /api/profile/proposals`

用途：读取 Profile proposal 列表。

成功响应：`ProfileProposal[]`。数据库不存在时返回空数组。

前端入口：`fetchProfileProposals()`。

### `POST /api/profile/proposals/generate`

用途：从当前 Profile 和 open feedback 启动反馈驱动 proposal job。

请求体：无。

成功响应：

```json
{"job": {"id":"...","job_type":"profile-proposal","status":"queued"}}
```

常见错误：

- 400：当前没有 Profile。

前端入口：`launchProfileProposalGeneration()`。

### `GET /api/profile/proposals/{id}`

用途：读取单条 proposal 详情。

路径参数：

- `id`：proposal id。

成功响应：`ProfileProposal`。

常见错误：404 proposal 不存在。

前端入口：当前前端主要读取列表，详情 handler 保持兼容。

### `POST /api/profile/proposals/{id}/apply`

用途：应用 proposal，可整份应用，也可按 change id 局部应用。

请求体可为空。局部应用时：

```json
{
  "accepted_change_ids": ["change-1"],
  "rejected_change_ids": ["change-2"]
}
```

成功响应：

```json
{"proposal": {"id": 1, "state": "applied"}, "job": {"id": "...", "job_type": "reclassify", "status": "queued"}}
```

`proposal` 为更新后的 `ProfileProposal`；`job` 仅在本次 apply 关联了需要重分类的 feedback 论文时出现，为对应的后台重分类任务。

副作用：

- 写入当前 Profile。
- proposal 标记为 applied。
- 关联 feedback 标记为 used。
- 如有关联 feedback 论文，启动后台 reclassify job（与 sync/手动 reclassify 共用 pipeline 互斥锁；若当前有任务在分类，job 保持 `queued` 排队，锁空闲后自动开始；可通过 cancel 接口停止），job 完成时重建最新报告。
- 如无关联论文，同步重建最新报告。

常见错误：

- 400：change id 非法或生成应用 Profile 失败。
- 404：proposal 不存在。
- 409：proposal 已 rejected，或当前 Profile version 已变化。

前端入口：`applyProfileProposal(id, selection)`。

### `POST /api/profile/proposals/{id}/reject`

用途：拒绝 proposal。

请求体：无。

成功响应：更新后的 `ProfileProposal`。

常见错误：404 proposal 不存在。

前端入口：`rejectProfileProposal(id)`。

## 6. Feedback

### `GET /api/feedback`

用途：读取反馈记录。

成功响应：`FeedbackRecord[]`。数据库不存在时返回空数组。

前端入口：`fetchFeedback()`。

### `POST /api/feedback`

用途：为论文创建纠错反馈。

请求体：

```json
{
  "paper_id": 1,
  "corrected_relevance": "direct",
  "note": "应归为直接相关"
}
```

成功响应：`FeedbackRecord`。

常见错误：

- 400：论文没有 classification 或 corrected relevance 不合法。
- 404：论文不存在。

前端入口：`createFeedback(input)`。

### `DELETE /api/feedback/{id}`

用途：删除反馈记录。

路径参数：

- `id`：feedback id。

成功响应：

```json
{"deleted":true,"feedback_id":1}
```

常见错误：404 feedback 不存在。

前端入口：`deleteFeedback(feedbackId)`。

## 7. Zotero

### `GET /api/zotero/collections`

用途：读取 Zotero collection 列表和默认 collection。

成功响应：

```json
{
  "collections": [
    {
      "key": "COLL-1",
      "name": "Papers",
      "path_label": "Library / Papers",
      "depth": 1,
      "parent_key": null,
      "is_default": true
    }
  ],
  "default_collection_key": "COLL-1"
}
```

常见错误：

- 400：Zotero API key、library type 或 library ID 配置错误，或远端请求失败。

前端入口：`fetchZoteroCollections()`。

### `POST /api/zotero/save/{paper_id}`

用途：保存论文到 Zotero，并写入本地保存状态。

路径参数：

- `paper_id`：paper id。

请求体：

```json
{"collection_key":"COLL-1"}
```

`collection_key` 可为 `null`，此时使用默认 collection 或 library 根。

成功响应：`ZoteroStatus`。

说明：Zotero 保存失败时，接口仍可能返回 200，并在 `ZoteroStatus` 中写入 `state: "error"` 和 `last_error`，用于前端展示。

常见错误：

- 400：论文没有 classification。
- 404：论文不存在。
- 500：SQLite 写入失败。

前端入口：`saveToZotero(paperId, collectionKey)`。

前端调用模板：`/api/zotero/save/${paperId}`。

## 8. Admin Jobs

### `POST /api/admin/run`

用途：启动一次完整同步。

请求体：无。

成功响应：

```json
{"job": {"id":"...","job_type":"sync","status":"queued"}, "reused": false}
```

sync 启动采用 single-flight 语义。已有 sync 处于 `queued`、`running` 或 `waiting_for_user` 时，重复请求返回 `200`、同一个 `job`，并设置 `reused: true`；不会启动第二次完整或定向同步。任务进入 `completed`、`failed` 或 `cancelled` 后可以再次启动。sync 与 reclassify 共用 pipeline 互斥锁：有 reclassify 正在分类时启动 sync 返回 `409`。

job result 典型字段：

- `fetched`
- `inserted`
- `updated`
- `classified`
- `errors`

可能状态：

- `queued`
- `running`
- `waiting_for_user`
- `completed`
- `failed`
- `cancelled`

前端入口：`launchAdminJob("/api/admin/run")`。

### `GET /api/admin/reclassify`

用途：读取重分类范围预览：当前数据库文章总数及各范围按"已有分类/尚未分类"的拆分，供指定数量控件设置合法上限并在确认前展示实际工作量。

可选查询参数：`limit`（0 到当前数据库文章总数），用于计算指定数量范围的拆分；省略时 `count_*` 字段按 0 计算。

成功响应：

```json
{
  "paper_count": 1234,
  "classified_paper_count": 1100,
  "unclassified_paper_count": 134,
  "today_paper_count": 130,
  "today_classified_count": 28,
  "today_unclassified_count": 102,
  "count_paper_count": 50,
  "count_classified_count": 40,
  "count_unclassified_count": 10
}
```

### `POST /api/admin/reclassify`

用途：按 scope 启动重分类 job。

请求体：

```json
{"scope":"count","limit":50}
```

`scope` 可选：

- `today`：按服务所在机器的本地自然日选择当天入库的文章
- `feedback`
- `all`
- `count`：按入库时间倒序选择指定数量
- `unclassified`：选择尚无分类记录的文章，用于补跑被取消或失败的批次

只有 `count` 使用 `limit`；合法范围为 0 到当前数据库文章总数。其他 scope 会忽略并归零 `limit`。

成功响应：

```json
{"job": {"id":"...","job_type":"reclassify","status":"queued"}}
```

reclassify 与 sync 共用 pipeline 互斥锁，同一时刻只允许一个分类任务执行。有 sync 或 reclassify 正在分类时启动返回 `409`；排队中的 reclassify（如 apply proposal 触发的 feedback 重分类）不算冲突，会等锁空闲后自动开始。

常见错误：

- 400：scope 非法、limit 超出范围或 JSON 无效。
- 409：sync 或 reclassify 正在执行分类工作。

前端入口：`launchReclassifyJob(input)`。

### `GET /api/admin/jobs`

用途：读取当前内存 job registry，按创建时间倒序。

成功响应：`JobInfo[]`。

LLM job 在完成或失败后包含可选 `llm_usage`：请求数、三类 token、模型、pricing 状态、价格快照和格式化人民币估算。DeepSeek 与 GLM 官方 endpoint 的已知模型可估价；没有精确 usage、未知模型或非官方 endpoint 时 `pricing_status` 为 `unavailable`，不会返回伪造金额。

前端入口：`fetchJobs()`。

### `GET /api/admin/jobs/{id}`

用途：读取单个 job 状态。

路径参数：

- `id`：job id。

成功响应：`JobInfo`。

常见错误：404 job 不存在。

前端入口：`fetchJob(id)`。

### `POST /api/admin/jobs/{id}/cancel`

用途：停止当前正在执行的 sync 或 reclassify job。停止请求会取消网络/LLM 请求和重试等待；如果 sync 正在等待受保护 feed 验证，也会关闭该次等待。已写入数据库的内容和已完成的分类批次不会回滚；取消的任务保留阶段性 result（fetched/inserted/updated/classified、warnings 等），Activity 面板据此显示已完成的数量与警告。

仅 `sync` 与 `reclassify` 且状态为 `queued`、`running` 或 `waiting_for_user` 的任务可停止。成功接受停止请求返回 `202`：

```json
{"job":{"id":"...","job_type":"sync","status":"running","cancel_requested":true},"cancel_requested":true}
```

任务最终状态为 `cancelled`；重复停止已结束任务返回 `200` 并设置 `already_terminal: true`。其他类型任务返回 400，未知 job 返回 404。

前端入口：`cancelAdminJob(id)`。

### `GET /api/admin/llm-usage?since=<RFC3339>`

用途：读取 SQLite 中按 job 汇总的 LLM usage ledger，按完成时间倒序返回。Dashboard 使用当前时间往前 3 天作为 `since`；数据库本身不自动清理历史。

每条记录包含：

- `job_id`、`job_type`、`status`、`model`、`completed_at`
- `request_count`
- `prompt_tokens`、`prompt_cache_hit_tokens`、`prompt_cache_miss_tokens`、`completion_tokens`
- `pricing_status`、`pricing` 单价快照
- 可用时返回 `estimated_cost_nano_cny` 和 `estimated_cost_cny`

费用仅对官方 `api.deepseek.com` 的已知模型计算，并按每个成功响应发生时的北京时间选择峰谷价格。高峰时段为周一至周五 9:00–12:00、14:00–18:00，其余时间（包括周末）为空闲时段。V4 Flash（以及兼容映射的 `deepseek-chat`、`deepseek-reasoner`）空闲价默认为命中 ¥0.05/M、未命中 ¥1.5/M、输出 ¥4.5/M，高峰价默认为 ¥0.10/M、¥3/M、¥9/M；V4 Pro 空闲价默认为 ¥0.15/M、¥4.5/M、¥13.5/M，高峰价默认为 ¥0.30/M、¥9/M、¥27/M。用户可以在 Settings → Model 手动调整这些默认值。每个 job 启动时锁定当时设置，ledger 保存实际采用的 `tier` 和费率快照，后续调价不回算历史。默认价格依据 DeepSeek 的[响应 usage 定义](https://api-docs.deepseek.com/api/create-chat-completion/)和[官方价格页](https://api-docs.deepseek.com/zh-cn/quick_start/pricing)。

数据库启动修复会幂等纠正 2026-08-22 起误用 `deepseek-cny-2026-07-24` 的记录，以及曾使用 `deepseek-cny-2026-08-21` 高峰价计算的周末记录；其他历史快照不自动改写。用户之后手动调整价格也不会触发历史回算。

常见错误：

- 400：缺少 `since` 或不是 RFC3339。
- 500：SQLite 查询失败。

前端入口：`fetchLLMUsage(since)`。

## 9. Feed Verification

这些接口只在 sync job 进入 `waiting_for_user` 且 `verification_required=true` 时有效。

### `POST /api/feeds/verification/start`

用途：启动原生 WebView2 验证器。

请求体：

```json
{"job_id":"...","feed_url":"https://example.com/rss"}
```

成功响应：

```json
{"ok":true,"verification_id":"..."}
```

副作用：

- 结束旧验证器进程。
- 启动新的 native verifier。
- job 状态保持 `waiting_for_user`。
- `verification_method` 设为 `native_webview2`。

常见错误：

- 400：job 未等待验证或启动验证器失败。
- 404：job 或 verification request 不存在。

前端入口：`startFeedVerification(input)`。

### `POST /api/feeds/verification/browser`

用途：打开系统浏览器作为手动验证 fallback。

请求体：

```json
{"job_id":"...","feed_url":"https://example.com/rss"}
```

成功响应：

```json
{"ok":true,"verification_id":"..."}
```

副作用：

- `verification_method` 设为 `browser_manual`。
- 前端提示用户复制最终 RSS/Atom/RDF XML。

常见错误：

- 400：job 未等待验证。
- 404：job 或 verification request 不存在。
- 500：打开浏览器失败。

前端入口：`openFeedVerificationInBrowser(input)`。

### `POST /api/feeds/verification/callback`

用途：验证器回调接口。通常由原生 helper 调用，前端不直接调用。

请求体主要字段：

```json
{
  "verification_id": "...",
  "status": "success",
  "content_type": "application/xml",
  "feed_xml": "<rss>...</rss>"
}
```

成功响应：ack payload，至少包含 `ok` 和 verification 信息。

常见错误：

- 400：payload 无效、XML 校验失败或状态不合法。
- 404：verification request 不存在。

前端入口：无。调用方是 `cmd/feedmedaily-protected-verifier`。

### `POST /api/feeds/verification/complete`

用途：让后端完成当前验证流程并把结果送回暂停的 sync。

请求体：

```json
{"job_id":"...","feed_url":"https://example.com/rss"}
```

成功响应：

```json
{"ok":true,"verification_id":"..."}
```

常见错误：

- 400：job 未等待验证，或完成验证失败。
- 404：job 或 verification request 不存在。

前端入口：`completeFeedVerification(input)`。

### `POST /api/feeds/verification/manual-submit`

用途：提交用户手动粘贴的 RSS/Atom/RDF XML。

请求体：

```json
{
  "job_id": "...",
  "feed_url": "https://example.com/rss",
  "feed_xml": "<rss>...</rss>"
}
```

成功响应：

```json
{"ok":true,"verification_id":"..."}
```

副作用：

- 后端校验 XML。
- 通过同一 callback path 注入 XML。
- 恢复暂停的 sync。

常见错误：

- 400：XML 为空、XML 不是有效 feed、job 未等待验证。
- 404：job 或 verification request 不存在。

前端入口：`submitFeedVerificationXML(input)`。

## 10. Static Frontend

### `GET /...`

用途：服务 `web/dist` 静态文件。找不到具体文件时返回 `index.html` 作为 SPA fallback。

说明：

- `/api/...` 未匹配路由返回 404。
- 非 GET/HEAD 静态请求返回 405。
- `web/dist/index.html` 不存在时返回一个最小 HTML 提示，说明需要先构建前端。

前端入口：浏览器直接访问本地服务。

## 11. API 维护清单

新增或修改 API 时必须同步检查：

1. `internal/api/server.go` 是否注册了正确路由和方法。
2. `web/src/api/client.ts` 是否封装了前端调用。
3. `web/src/shared/types.ts` 是否更新请求或响应类型。
4. `internal/api/*_test.go` 是否覆盖成功和错误路径。
5. `docs/api.zh-CN.md` 是否记录方法、请求体、响应体、错误和前端入口。
6. 如果影响首屏论文列表，确认没有阻塞 `/api/report/latest`。
