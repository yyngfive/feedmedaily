# FeedMeDaily 本地 API 文档

本文记录 FeedMeDaily 当前本地 HTTP API。服务由 `cmd/feedmedailyd` 提供，路由注册在 `internal/api/server.go`，前端调用集中在 `web/src/reportData.ts`。

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
  status: "queued" | "running" | "waiting_for_user" | "completed" | "failed";
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

前端入口：当前主要由托盘或外部探活使用，`web/src/reportData.ts` 未封装此接口。

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

用途：读取可编辑配置字段和每个字段来源。

请求体：无。

成功响应：

```json
{
  "fields": [
    {
      "key": "SCIRSS_CLASSIFIER_MODEL",
      "label": "Classifier model",
      "description": "Model name used for paper classification.",
      "section": "Classifier model",
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
  ]
}
```

secret 字段不以明文返回。

前端入口：`fetchSettingsConfig()`。

### `PUT /api/settings/config`

用途：更新本地配置，并立即重载后端 settings。

请求体：

```json
{
  "fields": {
    "SCIRSS_CLASSIFIER_MODEL": {"value": "deepseek-v4-flash"},
    "SCIRSS_ZOTERO_API_KEY": {"clear": true}
  }
}
```

成功响应：同 `GET /api/settings/config`。

常见错误：

- 400：无效 JSON、未知字段、非法值。
- 500：更新后 settings 重载失败。

前端入口：`saveSettingsConfig(fields)`。

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
- `platform`
- `automatic_supported`
- `advisory`
- `command`

Linux source mode 只保存设置，不由 Web UI 自动执行；响应会给出 helper 命令。

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

成功响应：更新后的 `ProfileProposal`。

副作用：

- 写入当前 Profile。
- proposal 标记为 applied。
- 关联 feedback 标记为 used。
- 重分类关联反馈论文。
- 重建最新报告。

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
{"job": {"id":"...","job_type":"sync","status":"queued"}}
```

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

前端入口：`launchAdminJob("/api/admin/run")`。

### `POST /api/admin/reclassify`

用途：按 scope 启动重分类 job。

请求体：

```json
{"scope":"recent","limit":50}
```

`scope` 可选：

- `recent`
- `feedback`
- `all`

`limit` 默认 50，合法范围 1 到 500。

成功响应：

```json
{"job": {"id":"...","job_type":"reclassify","status":"queued"}}
```

常见错误：

- 400：scope 非法、limit 超出范围或 JSON 无效。

前端入口：`launchReclassifyJob(input)`。

### `GET /api/admin/jobs`

用途：读取当前内存 job registry，按创建时间倒序。

成功响应：`JobInfo[]`。

前端入口：`fetchJobs()`。

### `GET /api/admin/jobs/{id}`

用途：读取单个 job 状态。

路径参数：

- `id`：job id。

成功响应：`JobInfo`。

常见错误：404 job 不存在。

前端入口：`fetchJob(id)`。

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
2. `web/src/reportData.ts` 是否封装了前端调用。
3. `web/src/types.ts` 是否更新请求或响应类型。
4. `internal/api/*_test.go` 是否覆盖成功和错误路径。
5. `docs/api.zh-CN.md` 是否记录方法、请求体、响应体、错误和前端入口。
6. 如果影响首屏论文列表，确认没有阻塞 `/api/report/latest`。
