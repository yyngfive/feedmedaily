# FeedMeDaily Go 迁移手工测试清单

这份文档面向当前已经迁移到 Go 的运行时、模块、API 和命令，目标是让你可以：

- 手动打开命令行逐条跑命令
- 手动打开网页 UI 输入内容、点按钮
- 手动调用 HTTP API 做精确验证
- 快速判断某个功能是否真的已经由 Go 接管

本文默认你在源码仓库根目录运行：

`D:\Codes\Projects\SciRSSAgent`

## 1. 测试范围

当前建议重点回归这些已经迁到 Go 的能力：

- Go 后端服务 `feedmedailyd`
- Go 托盘 `feedmedaily-tray`
- 本地 Web UI 和静态资源
- RSS 抓取、入库、分类、报告生成
- report / profile / feedback / proposals API
- proposal bootstrap / generate / apply / reject
- reclassify job
- Zotero collections / save
- 本地设置、RSS feeds、scheduler 设置

## 2. 前置条件

### 2.1 最低前置

- 已安装 Go
- 已安装 Node + pnpm
- 仓库根目录存在 `.env`

如果还没准备好：

```powershell
Copy-Item .env.example .env
pnpm --dir web install
pnpm --dir web build
```

### 2.2 按功能分级的前置条件

#### A. 不需要任何外部 API key 也能测

- 托盘启动/停止
- 后端启动/退出
- 静态页面打开
- `/api/app/*`
- `/api/settings/feeds`
- `/api/settings/scheduler`
- 无 profile 时的 onboarding UI

#### B. 需要分类/画像模型配置

这些功能依赖 `.env` 中至少配置好：

- `SCIRSS_CLASSIFIER_API_KEY`
- `SCIRSS_CLASSIFIER_BASE_URL`
- `SCIRSS_CLASSIFIER_MODEL`
- `SCIRSS_PROFILE_API_KEY`
- `SCIRSS_PROFILE_BASE_URL`
- `SCIRSS_PROFILE_MODEL`

相关功能：

- `Generate initial profile`
- `Generate profile proposal`
- `Sync now`
- `Reclassify recent / feedback / all`

#### C. 需要 Zotero 配置

这些功能还需要：

- `SCIRSS_ZOTERO_API_KEY`
- `SCIRSS_ZOTERO_LIBRARY_TYPE`
- `SCIRSS_ZOTERO_LIBRARY_ID`

相关功能：

- `/api/zotero/collections`
- `/api/zotero/save/{paper_id}`

## 3. 建议的测试顺序

如果你只想快速确认“主流程能不能跑”，按这个顺序测：

1. 启动 Go 后端
2. 打开网页 UI
3. 检查 `/api/app/health`
4. 配置模型设置
5. 添加 RSS feeds
6. 生成初始 profile
7. 跑一次 `Sync now`
8. 在右侧详情区测试 `Mark as read` / `Mark wrong`
9. 在 Admin 里测试 `Generate proposal`
10. 测试 `Apply` 或 `Reject`
11. 测试 `Reclassify recent`
12. 测试 Zotero 列表和保存

## 4. 启动方式

### 4.1 直接启动 Go 后端

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

预期结果：

- 命令不立即退出
- 浏览器访问 [http://127.0.0.1:8000](http://127.0.0.1:8000) 可打开页面

### 4.2 启动 Go 托盘

```powershell
go run .\cmd\feedmedaily-tray --root .
```

预期结果：

- 系统托盘出现 FeedMeDaily 图标
- 右键菜单可见
- 双击或点击 `Open FeedMeDaily` 会拉起浏览器

## 5. 5 分钟冒烟测试

### 5.1 后端健康检查

```powershell
$base = "http://127.0.0.1:8000"
Invoke-RestMethod "$base/api/app/health"
```

预期结果：

- 返回 JSON
- `status = "ok"`
- `name = "FeedMeDaily"`

### 5.2 打开 UI

浏览器打开：

[http://127.0.0.1:8000](http://127.0.0.1:8000)

预期结果：

- 有页面，不是 404
- 第一次没有 profile 时应进入 onboarding

### 5.3 报告读取

```powershell
Invoke-RestMethod "$base/api/report/latest"
```

预期结果：

- 返回结构完整的报告 JSON
- 即使没有数据，也至少包含：
  - `generated_at`
  - `report_date`
  - `totals`
  - `papers`
  - `errors`

## 6. 命令行测试

## 6.1 服务模式命令

### 6.1.1 启动后端

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

检查点：

- 服务不崩溃
- `runtime.json` 被写入
- `/api/app/meta` 返回 `process_running = true`

### 6.1.2 daemon 只负责服务启动

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

预期结果：

- 进程保持运行，不会执行一次性 sync 后立即退出
- `Sync now`、托盘 `Run Sync Now`、以及 Linux helper script 都会通过 `/api/admin/run` 触发后台 job
- Web UI 可以看到统一的 job 状态

### 6.1.3 Zotero 相关能力改走 HTTP API

请改用下面第 7.4 节的 `/api/zotero/*` 接口验证，不再通过 `feedmedailyd` 一次性命令参数测试。

## 6.2 托盘命令和菜单手测

启动：

```powershell
go run .\cmd\feedmedaily-tray --root .
```

逐项测试：

### 6.2.1 `Open FeedMeDaily`

操作：

- 右键托盘图标
- 点击 `Open FeedMeDaily`

预期结果：

- 如果服务没启动，会自动启动服务
- 浏览器打开本地 UI

### 6.2.2 `Run Sync Now`

操作：

- 右键托盘图标
- 点击 `Run Sync Now`

预期结果：

- 托盘出现提示
- UI 的 Jobs 区能看到新的 `sync` job

### 6.2.3 `Quit Tray And Stop Service`

操作：

- 右键托盘图标
- 点击 `Quit Tray And Stop Service`

预期结果：

- 托盘退出
- `http://127.0.0.1:8000/api/app/health` 访问失败

### 6.2.4 `Open Data Folder` / `Open Logs Folder`

操作：

- 右键托盘图标
- 点击对应菜单

预期结果：

- Windows 资源管理器打开正确目录

### 6.2.5 `Launch at login`

操作：

- 右键托盘图标
- 切换 `Launch at login`

预期结果：

- 托盘提示启用或禁用成功

### 6.2.6 `Enable Daily Sync (...)`

操作：

- 右键托盘图标
- 切换 daily sync

预期结果：

- 托盘提示启用或禁用成功
- [tray-settings.json](D:/Codes/Projects/SciRSSAgent/tray-settings.json) 的调度字段变化

说明：

当前迁移分支的 scheduler 实际落在本地 tray 设置，而不是 Windows Task Scheduler。UI 和测试都应以 tray-local daily sync 为准。

## 7. HTTP API 手工测试

以下命令默认服务已启动：

```powershell
$base = "http://127.0.0.1:8000"
```

## 7.1 App API

### 7.1.1 `GET /api/app/health`

```powershell
Invoke-RestMethod "$base/api/app/health"
```

检查点：

- `status = ok`
- 有 `version`
- 有 `mode`

### 7.1.2 `GET /api/app/meta`

```powershell
Invoke-RestMethod "$base/api/app/meta"
```

检查点：

- `install_dir`
- `data_dir`
- `logs_dir`
- `config_dir`
- `process_running = true`

### 7.1.3 `GET /api/app/update`

```powershell
Invoke-RestMethod "$base/api/app/update"
Invoke-RestMethod "$base/api/app/update?force=1"
```

检查点：

- 如果没配置更新地址，`status` 通常为 `not_configured`
- 如果配置了更新地址，会返回 `has_update`
- `force=1` 会立即重查远程 manifest，而不是继续复用 5 分钟成功缓存
- 返回体里的 `checked_at` 会反映本次实际检查时间

### 7.1.4 `POST /api/app/open`

测试打开数据目录：

```powershell
Invoke-RestMethod "$base/api/app/open" -Method Post -ContentType "application/json" -Body '{"target":"data_dir"}'
```

也可以测试这些 target：

- `logs_dir`
- `install_dir`
- `server_url`
- `download_url`
- `release_notes_url`

预期结果：

- 返回 `ok = true`
- 对应目录或链接被打开

### 7.1.5 `POST /api/app/exit`

```powershell
Invoke-RestMethod "$base/api/app/exit" -Method Post
```

预期结果：

- 先返回成功 JSON
- 随后服务退出

## 7.2 Settings API

### 7.2.1 `GET /api/settings/config`

```powershell
Invoke-RestMethod "$base/api/settings/config"
```

检查点：

- 返回 `fields`
- 每个字段有 `key`、`label`、`section`、`source`

### 7.2.2 `PUT /api/settings/config`

建议只改一个无害字段，例如 classifier batch size：

```powershell
$body = @{
  fields = @{
    SCIRSS_CLASSIFIER_BATCH_SIZE = @{
      value = "10"
    }
  }
} | ConvertTo-Json -Depth 6

Invoke-RestMethod "$base/api/settings/config" -Method Put -ContentType "application/json" -Body $body
```

预期结果：

- 返回更新后的 `fields`
- 页面刷新后仍能读到同值

### 7.2.3 `GET /api/settings/feeds`

```powershell
Invoke-RestMethod "$base/api/settings/feeds"
```

预期结果：

- 返回 feed 列表

### 7.2.4 `PUT /api/settings/feeds`

示例：

```powershell
$body = @{
  feeds = @(
    @{
      journal = "Example Feed"
      url = "https://example.com/rss.xml"
    }
  )
} | ConvertTo-Json -Depth 5

Invoke-RestMethod "$base/api/settings/feeds" -Method Put -ContentType "application/json" -Body $body
```

预期结果：

- 返回保存后的 feeds
- [data/rss_feeds.json](D:/Codes/Projects/SciRSSAgent/data/rss_feeds.json) 被更新

### 7.2.5 `GET /api/settings/scheduler`

```powershell
Invoke-RestMethod "$base/api/settings/scheduler"
```

检查点：

- 有 `installed`
- 有 `scheduled_time`
- 有 `command`

### 7.2.6 `PUT /api/settings/scheduler`

```powershell
Invoke-RestMethod "$base/api/settings/scheduler" -Method Put -ContentType "application/json" -Body '{"daily_time":"09:30"}'
```

预期结果：

- `installed = true`
- `scheduled_time = "09:30"`

### 7.2.7 `DELETE /api/settings/scheduler`

```powershell
Invoke-RestMethod "$base/api/settings/scheduler" -Method Delete
```

预期结果：

- `installed = false`

## 7.3 Profile / Feedback / Proposal API

### 7.3.1 `GET /api/profile/current`

```powershell
Invoke-RestMethod "$base/api/profile/current"
```

预期结果：

- 没有 profile 时返回 `{"profile": null}`
- 有 profile 时返回完整 profile 文档

### 7.3.2 `POST /api/profile/bootstrap`

```powershell
$body = @{
  interest_description = "I track machine learning systems, AI agents, LLM evaluation, retrieval, and scientific literature tooling."
  name = "ML Systems"
} | ConvertTo-Json -Depth 5

Invoke-RestMethod "$base/api/profile/bootstrap" -Method Post -ContentType "application/json" -Body $body
```

预期结果：

- 返回 `{ job: ... }`
- 之后轮询 job 会完成或失败

### 7.3.3 `GET /api/feedback`

```powershell
Invoke-RestMethod "$base/api/feedback"
```

### 7.3.4 `POST /api/feedback`

先从 report 找一个 paper id，再执行：

```powershell
$body = @{
  paper_id = 1
  corrected_relevance = "indirect"
  note = "Manual regression test"
} | ConvertTo-Json -Depth 5

Invoke-RestMethod "$base/api/feedback" -Method Post -ContentType "application/json" -Body $body
```

预期结果：

- 返回 feedback record
- `original_relevance` 取自 latest classification

### 7.3.5 `DELETE /api/feedback/{id}`

```powershell
Invoke-RestMethod "$base/api/feedback/1" -Method Delete
```

预期结果：

- 返回 `deleted = true`

### 7.3.6 `POST /api/papers/{id}/read`

```powershell
Invoke-RestMethod "$base/api/papers/1/read" -Method Post
```

预期结果：

- 返回 `paper_id`
- 返回 `read_at`

### 7.3.7 `GET /api/profile/proposals`

```powershell
Invoke-RestMethod "$base/api/profile/proposals"
```

### 7.3.8 `POST /api/profile/proposals/generate`

```powershell
Invoke-RestMethod "$base/api/profile/proposals/generate" -Method Post
```

预期结果：

- 返回 `{ job: ... }`

### 7.3.9 `GET /api/profile/proposals/{id}`

```powershell
Invoke-RestMethod "$base/api/profile/proposals/1"
```

### 7.3.10 `POST /api/profile/proposals/{id}/apply`

```powershell
Invoke-RestMethod "$base/api/profile/proposals/1/apply" -Method Post
```

预期结果：

- proposal `state = applied`
- profile 文件 version 增加
- 关联 feedback 会被标为 `used`

### 7.3.11 `POST /api/profile/proposals/{id}/reject`

```powershell
Invoke-RestMethod "$base/api/profile/proposals/1/reject" -Method Post
```

预期结果：

- proposal `state = rejected`

## 7.4 Zotero API

### 7.4.1 `GET /api/zotero/collections`

```powershell
Invoke-RestMethod "$base/api/zotero/collections"
```

预期结果：

- 有 `collections`
- collection 里有 `path_label`

### 7.4.2 `POST /api/zotero/save/{paper_id}`

```powershell
Invoke-RestMethod "$base/api/zotero/save/1" -Method Post -ContentType "application/json" -Body '{"collection_key":null}'
```

预期结果：

- 成功时 `saved = true`
- 失败时返回状态对象，通常带 `last_error`

## 7.5 Admin Job API

### 7.5.1 `POST /api/admin/run`

```powershell
$job = (Invoke-RestMethod "$base/api/admin/run" -Method Post).job
$job
```

轮询：

```powershell
Invoke-RestMethod "$base/api/admin/jobs/$($job.id)"
```

预期结果：

- job `job_type = sync`
- `status` 会经历 `queued -> running -> completed/failed`

### 7.5.2 `POST /api/admin/reclassify`

```powershell
$body = @{
  scope = "recent"
  limit = 50
} | ConvertTo-Json

$job = (Invoke-RestMethod "$base/api/admin/reclassify" -Method Post -ContentType "application/json" -Body $body).job
$job
```

也可测：

- `scope = "feedback"`
- `scope = "all"`

### 7.5.3 `GET /api/admin/jobs`

```powershell
Invoke-RestMethod "$base/api/admin/jobs"
```

### 7.5.4 `GET /api/admin/jobs/{id}`

```powershell
Invoke-RestMethod "$base/api/admin/jobs/$($job.id)"
```

## 8. Web UI 手工测试

默认页面：

[http://127.0.0.1:8000](http://127.0.0.1:8000)

## 8.1 首次 onboarding

适用前置：

- `data/classification_profile.json` 不存在

步骤：

1. 打开页面
2. 看到 onboarding
3. 在 `Profile name` 输入任意名称
4. 在 interests 文本框输入研究兴趣
5. 点击 `Generate initial profile`

预期结果：

- 页面提示 profile generation job 已启动
- 生成成功后，`Latest proposal` 区出现 proposal
- 点击 `Apply` 后，profile 生效

## 8.2 Settings / Config

步骤：

1. 点击右上 `Settings`
2. 保持在 `Config` tab
3. 修改一个安全字段，例如 batch size
4. 点击保存

预期结果：

- 保存成功
- 刷新页面后值仍存在

## 8.3 Feeds tab

步骤：

1. 打开 `Settings`
2. 切到 `Feeds`
3. 添加 1 条 RSS 订阅
4. 点击保存

预期结果：

- 列表保存成功
- 刷新页面后仍在

## 8.4 Scheduler

步骤：

1. 在 `Settings` 或 Admin 中找到 scheduler 区
2. 输入一个时间，例如 `09:30`
3. 保存
4. 再删除/禁用

预期结果：

- UI 显示启用状态
- 再删除后恢复为未启用

## 8.5 `Sync now`

步骤：

1. 打开 Admin
2. 点击 `Sync now`

预期结果：

- Jobs 区出现 `sync` job
- 依次出现进度：
  - fetching
  - metadata
  - classifying
  - report refreshing
- 完成后主列表出现论文

## 8.6 论文阅读动作

前置：

- 列表里已有 paper

步骤：

1. 在中间列表点开一篇 paper
2. 右侧点击 `Mark as read`

预期结果：

- 按钮变成 `Read`
- 过滤切回 `Unread` 时，这篇应消失

## 8.7 `Mark wrong`

步骤：

1. 打开右侧详情
2. 点击 `Mark wrong`
3. 选择新的 relevance
4. 输入 note
5. 提交

预期结果：

- 反馈保存成功
- Admin 的 `Profile + Feedback` 区能看到这条反馈

## 8.8 删除 feedback

步骤：

1. 打开 Admin
2. 切到 `Profile + Feedback`
3. 找到一条 feedback
4. 删除

预期结果：

- 列表中消失

## 8.9 Generate proposal

前置：

- 已有 profile
- 已有 open feedback

步骤：

1. 打开 Admin
2. 点击 `Generate proposal`

预期结果：

- 出现新的 `profile-proposal` job
- 完成后 proposal 列表刷新

## 8.10 Apply proposal

步骤：

1. 在 pending proposal 上点击 `Apply`

预期结果：

- proposal 状态变 `applied`
- 当前 profile 文档刷新
- 关联 paper 被重新分类
- report 摘要刷新

## 8.11 Reject proposal

步骤：

1. 在 pending proposal 上点击 `Reject`

预期结果：

- proposal 状态变 `rejected`

## 8.12 Reclassify

按钮位置：

- `Reclassify recent 50`
- `Reclassify feedback papers`
- `Reclassify all`

步骤：

1. 各点一次

预期结果：

- 每次都会产生新的 reclassify job
- job 完成后 report 摘要刷新

## 8.13 Save to Zotero

前置：

- 已配置 Zotero
- 当前 paper 已有 classification

步骤：

1. 打开 paper 详情
2. 点击 `Save to Zotero`
3. modal 打开后选择 collection
4. 点击保存

预期结果：

- 成功时提示 `Saved to Zotero.`
- 该 paper 的按钮状态变 `Saved`

## 8.14 Update / Open actions

可测内容：

- 打开 data folder
- 打开 logs folder
- 打开 install dir
- 检查 update 状态

预期结果：

- 路径正确被打开

## 9. 输出文件和状态文件检查

这些检查主要用于确认本地状态确实落盘了：

- [data/literature.sqlite](D:/Codes/Projects/SciRSSAgent/data/literature.sqlite)
- [data/rss_feeds.json](D:/Codes/Projects/SciRSSAgent/data/rss_feeds.json)
- [data/classification_profile.json](D:/Codes/Projects/SciRSSAgent/data/classification_profile.json)
- [tray-settings.json](D:/Codes/Projects/SciRSSAgent/tray-settings.json)

典型对应关系：

- 保存 feeds 后看 `rss_feeds.json`
- apply proposal 后看 `classification_profile.json`
- scheduler 改动后看 `tray-settings.json`

## 10. 常见失败现象与判断

### 10.1 页面能打开但没有数据

先检查：

- `/api/app/health`
- `/api/report/latest`
- 是否还没跑过 `Sync now`

### 10.2 profile 相关按钮报错

先检查：

- `.env` 是否配置了 `SCIRSS_PROFILE_*`
- 当前是否已经有 profile
- 是否已经有 feedback

### 10.3 run/reclassify 报错

先检查：

- `.env` 是否配置了 `SCIRSS_CLASSIFIER_*`
- feed 是否有效
- 外部 API 是否可访问

### 10.4 Zotero 报错

先检查：

- `SCIRSS_ZOTERO_API_KEY`
- `SCIRSS_ZOTERO_LIBRARY_TYPE`
- `SCIRSS_ZOTERO_LIBRARY_ID`
- 该 paper 是否已经有 classification

## 11. 建议的回归结论模板

你每次手测完，可以按下面格式记录：

```text
版本/分支：
测试日期：
测试环境：

1. 后端启动：通过 / 失败
2. 托盘启动：通过 / 失败
3. UI 打开：通过 / 失败
4. 设置保存：通过 / 失败
5. feeds 保存：通过 / 失败
6. profile bootstrap：通过 / 失败
7. Sync now：通过 / 失败
8. mark as read：通过 / 失败
9. mark wrong / feedback：通过 / 失败
10. proposal generate：通过 / 失败
11. proposal apply / reject：通过 / 失败
12. reclassify：通过 / 失败
13. zotero collections/save：通过 / 失败
14. report rebuild：通过 / 失败

备注：
```

## 12. 安装包 build 与安装测试

这一节用于验证：

- 源码能否成功构建 release 目录
- 安装器能否成功生成
- 安装器安装后的目录结构是否正确
- 安装后的 tray / backend / UI 是否正常
- 卸载是否基本干净

## 12.1 构建前准备

建议先关闭已经运行的 FeedMeDaily release 版程序，避免旧进程占用 `dist` 文件。

前置：

- `go` 可用
- `corepack pnpm` 可用
- 前端依赖已安装
- 如果要生成安装器，Windows 上安装了 Inno Setup 6，并且 `ISCC.exe` 可被 `tools/build_release.ps1` 找到

可选先确认 Inno Setup：

```powershell
Get-Command ISCC.exe -ErrorAction SilentlyContinue
```

如果这里拿不到结果，也可以检查：

- `C:\Program Files (x86)\Inno Setup 6\ISCC.exe`
- `C:\Program Files\Inno Setup 6\ISCC.exe`

## 12.2 只构建 release 目录，不打安装器

```powershell
pwsh -File .\tools\build_release.ps1 -SkipInstaller
```

预期结果：

- 命令成功退出
- 生成 [dist/FeedMeDaily](D:/Codes/Projects/SciRSSAgent/dist/FeedMeDaily)
- 不要求存在安装器 exe

重点检查这些文件：

- [dist/FeedMeDaily/FeedMeDailyTray.exe](D:/Codes/Projects/SciRSSAgent/dist/FeedMeDaily/FeedMeDailyTray.exe)
- [dist/FeedMeDaily/feedmedailyd.exe](D:/Codes/Projects/SciRSSAgent/dist/FeedMeDaily/feedmedailyd.exe)
- [dist/FeedMeDaily/feedmedaily.ico](D:/Codes/Projects/SciRSSAgent/dist/FeedMeDaily/feedmedaily.ico)
- [dist/FeedMeDaily/web/dist/index.html](D:/Codes/Projects/SciRSSAgent/dist/FeedMeDaily/web/dist/index.html)
- [dist/update.json](D:/Codes/Projects/SciRSSAgent/dist/update.json)

建议命令：

```powershell
Get-ChildItem .\dist\FeedMeDaily
Get-ChildItem .\dist\FeedMeDaily\web\dist
Get-Content .\dist\update.json
```

检查点：

- `FeedMeDailyTray.exe` 存在
- `feedmedailyd.exe` 存在
- `web/dist` 已复制进去
- `update.json` 的 `version` 与 `web/package.json` 一致
- `update.json` 的 `download_url` 仍指向 GitHub Releases 安装包
- 构建后需要把 `dist/update.json` 额外发布到主地址，供应用内更新检查读取

## 12.3 构建完整安装包

```powershell
pwsh -File .\tools\build_release.ps1
```

预期结果：

- 命令成功退出
- 生成 [dist/installer](D:/Codes/Projects/SciRSSAgent/dist/installer)

检查命令：

```powershell
Get-ChildItem .\dist\installer
```

预期至少有一个类似文件：

- `FeedMeDaily-v0.3.1.exe`

如果没有安装器但前面 release 目录有了，常见原因是：

- 没安装 Inno Setup
- `ISCC.exe` 不在脚本可发现的位置

## 12.4 安装器文件内容和命名检查

手工检查：

- 安装器文件名包含版本号
- 图标不是空白默认图标
- 文件大小明显大于 0

推荐命令：

```powershell
Get-Item .\dist\installer\FeedMeDaily-*.exe | Select-Object Name,Length,LastWriteTime
```

## 12.5 实际安装测试

### 12.5.1 启动安装器

在资源管理器中双击：

- [dist/installer](D:/Codes/Projects/SciRSSAgent/dist/installer)

里的 `FeedMeDaily-v*.exe`

或者 PowerShell：

```powershell
Start-Process .\dist\installer\FeedMeDaily-v0.3.1.exe
```

预期结果：

- 安装向导正常打开
- 标题显示 `FeedMeDaily`
- 图标正确显示

### 12.5.2 安装路径页

检查点：

- 默认安装目录类似：
  - `C:\Program Files\FeedMeDaily`
- 可以自定义目录

### 12.5.3 安装完成页

检查点：

- 安装完成页正常显示
- 有 `Launch FeedMeDaily tray` 之类的启动选项

## 12.6 安装后文件检查

安装完成后，检查安装目录。

默认路径通常是：

- `C:\Program Files\FeedMeDaily`

应至少存在：

- `FeedMeDailyTray.exe`
- `feedmedailyd.exe`
- `feedmedaily.ico`
- `web\dist\index.html`

可用命令：

```powershell
Get-ChildItem "C:\Program Files\FeedMeDaily"
Get-ChildItem "C:\Program Files\FeedMeDaily\web\dist"
```

## 12.7 安装后首次启动测试

### 12.7.1 从安装器完成页启动

预期结果：

- 托盘出现图标
- 浏览器自动打开或可以手动从托盘打开

### 12.7.2 从开始菜单启动

步骤：

1. 打开开始菜单
2. 搜索 `FeedMeDaily`
3. 点击快捷方式

预期结果：

- 启动 `FeedMeDailyTray.exe --root <install-dir>`
- 托盘出现

### 12.7.3 桌面快捷方式

如果安装时勾选了 desktop icon：

- 桌面应出现快捷方式
- 点击可启动托盘

## 12.8 安装后运行测试

这一步建议按“release 视角”做最小验证，不依赖源码目录。

### 12.8.1 检查 tray 是否能拉起 backend

步骤：

1. 托盘右键
2. 点击 `Open FeedMeDaily`

预期结果：

- 若服务未运行，会自动拉起 `feedmedailyd.exe`
- 浏览器打开本地 UI

### 12.8.2 检查本地 health

在安装后打开 PowerShell：

```powershell
Invoke-RestMethod "http://127.0.0.1:8000/api/app/health"
```

预期结果：

- 返回 `status = ok`

### 12.8.3 检查 app meta

```powershell
Invoke-RestMethod "http://127.0.0.1:8000/api/app/meta"
```

重点检查：

- `install_dir` 指向安装目录
- `process_running = true`

### 12.8.4 检查 release UI 静态资源

浏览器打开：

[http://127.0.0.1:8000](http://127.0.0.1:8000)

预期结果：

- 页面正常加载
- 不是“Frontend build not found”

## 12.9 安装后数据目录检查

安装版的数据目录应该落在：

```text
%LOCALAPPDATA%\FeedMeDaily\
```

推荐命令：

```powershell
Get-ChildItem "$env:LOCALAPPDATA\\FeedMeDaily" -Force
```

通常应看到这些子目录或文件逐步出现：

- `config`
- `data`
- `logs`
- `reports`

进一步检查：

```powershell
Get-ChildItem "$env:LOCALAPPDATA\\FeedMeDaily\\config" -Force
Get-ChildItem "$env:LOCALAPPDATA\\FeedMeDaily\\data" -Force
```

预期结果：

- 配置和用户数据写在 `LocalAppData`，而不是安装目录里

## 12.10 安装后功能最小回归

安装版至少建议做这几个动作：

1. 打开 UI
2. 打开 Admin
3. 保存一条 feed
4. 保存一项 scheduler 时间
5. 关闭并重新打开 tray
6. 再次打开 UI

预期结果：

- 上一步保存的数据还在
- tray 和 backend 可以再次拉起

## 12.11 重装覆盖测试

目的：

- 验证重新安装同版本或新版本时不会留下明显脏状态

步骤：

1. 保持已有安装
2. 再次运行安装器
3. 选择同一路径覆盖安装

预期结果：

- 安装能继续完成
- 安装目录中没有混入旧的 Python 运行壳文件
- 启动后 tray / backend / UI 仍正常

重点检查安装目录不要出现这些历史主入口痕迹：

- `FeedMeDaily.exe`
- `_internal`（如果存在应被清理）

## 12.12 卸载测试

步骤：

1. 打开 Windows `应用和功能`
2. 找到 `FeedMeDaily`
3. 执行卸载

预期结果：

- 程序文件从安装目录移除
- 开始菜单快捷方式消失
- 桌面快捷方式消失

安装目录复查：

```powershell
Test-Path "C:\Program Files\FeedMeDaily"
```

预期：

- 大多数情况下返回 `False`

说明：

- 用户数据目录 `%LOCALAPPDATA%\FeedMeDaily\` 是否保留，要按你产品期望判断
- 当前测试主要确认“卸载程序文件是否正确”

## 12.13 安装包回归结论建议

建议在测试记录里补这几项：

```text
15. release build（-SkipInstaller）：通过 / 失败
16. installer build：通过 / 失败
17. installer 启动：通过 / 失败
18. 安装目录文件：通过 / 失败
19. 安装后 tray 启动：通过 / 失败
20. 安装后 backend health：通过 / 失败
21. 安装后 UI 打开：通过 / 失败
22. 安装后 LocalAppData 写入：通过 / 失败
23. 覆盖安装：通过 / 失败
24. 卸载：通过 / 失败
```
