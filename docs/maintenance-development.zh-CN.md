# FeedMeDaily 维护与开发手册

本文面向刚接手 FeedMeDaily 的维护者。目标是让维护者能在不了解历史实现的情况下，完成本地运行、定位问题、修改功能、验证行为和准备发布。

## 1. 项目定位

FeedMeDaily 是一个本地优先的科研 RSS 论文筛选工具。它从期刊 RSS 抓取论文，把论文写入本地 SQLite，按用户 Profile 调用 LLM 分类，并在本地 Web UI 中提供阅读、反馈和 Zotero 保存能力。

核心工作流：

1. 用户维护 RSS 订阅源。
2. 后端抓取 RSS/Atom/RDF，解析论文条目并去重入库。
3. 缺失 DOI、作者、期刊、摘要等字段时，后端做条件式元数据补全。
4. 分类器模型根据 `data/classification_profile.json` 判断论文相关性。
5. UI 通过 `/api/report/latest` 读取最新论文列表。
6. 用户在右侧详情面板执行 `Mark as read`、`Mark wrong`、`Save to Zotero` 等动作。
7. 反馈记录可生成新的 Profile proposal，应用 proposal 后只重分类相关反馈论文。

## 2. 运行模式

项目有两种主要运行模式。排障时必须先判断当前是哪一种。

### Source mode

开发时使用 `--root .`。运行状态落在仓库目录下：

- 日志：`logs/`
- 用户数据：`data/`
- 数据库：`data/literature.sqlite`
- RSS 订阅：`data/rss_feeds.json`
- 当前 Profile：`data/classification_profile.json`
- 托盘设置：`tray-settings.json`

常用启动方式：

```powershell
go run .\cmd\feedmedaily-tray --root .
```

直接启动后端服务：

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

### Release mode

安装版由 Windows 托盘程序作为主要入口。运行状态落在：

```text
%LOCALAPPDATA%\FeedMeDaily\
```

排查安装版问题时，不要只看仓库内的 `logs/` 和 `data/`。应从 UI 的 app meta、托盘入口或 `%LOCALAPPDATA%\FeedMeDaily\logs` 找日志。

## 3. 本地开发环境

后端使用 Go，前端使用 Vite + React + TypeScript。推荐环境：

- Go：以 `go.mod` 中版本为准。
- Node：以 `.nvmrc` 和 `web/package.json` 的 engines 为准。
- pnpm：通过 Corepack 调用。
- Windows：完整托盘、WebView2 受保护订阅验证和安装包流程当前以 Windows 为主。

首次设置：

```powershell
Copy-Item .env.example .env
corepack pnpm --dir web install
corepack pnpm --dir web build
go run .\cmd\feedmedaily-tray --root .
```

前端开发：

```powershell
corepack pnpm --dir web install
corepack pnpm --dir web dev
corepack pnpm --dir web build
```

后端测试：

```powershell
go test ./...
```

前端类型检查和构建：

```powershell
corepack pnpm --dir web build
```

打包：

```powershell
.\tools\build_release.ps1
```

只构建 release 目录、不生成安装包：

```powershell
.\tools\build_release.ps1 -SkipInstaller
```

Linux 源码模式：

```bash
cp .env.example .env
corepack pnpm --dir web install
corepack pnpm --dir web build
bash ./tools/feedmedaily.sh serve
bash ./tools/feedmedaily.sh open
bash ./tools/feedmedaily.sh sync
bash ./tools/feedmedaily.sh paths
```

Linux 当前不提供托盘程序。定时同步推荐用 cron 调用 helper：

```cron
0 8 * * * cd /path/to/feedmedaily && bash /path/to/feedmedaily/tools/feedmedaily.sh sync >> /path/to/feedmedaily/logs/cron.log 2>&1
```

## 4. 目录职责

- `cmd/feedmedailyd/`：本地 Go 后端入口，负责 HTTP API、静态前端、同步任务和本地服务生命周期。
- `cmd/feedmedaily-tray/`：Windows 托盘入口，负责单实例、启动后端、打开 UI、同步触发、开机自启和本地调度。
- `cmd/feedmedaily-protected-verifier/`：Go 原生 WebView2 受保护订阅验证器。
- `internal/api/`：本地 HTTP API、job 状态、受保护订阅验证 API、静态资源服务。
- `internal/jobs/`：同步 pipeline、重分类、Profile proposal 后台作业。
- `internal/feeds/`：RSS/Atom/RDF 抓取、解析、订阅文件读写、特殊出版社抽取逻辑。
- `internal/classifier/`：LLM 分类请求、prompt 约束、thinking fallback。
- `internal/llmusage/`：每个 job 的线程安全 usage 累计、DeepSeek 单价快照和整数 nano-CNY 计算。
- `internal/profile/`：Profile 校验、生成、proposal 差异和写入。
- `internal/store/sqlite/`：SQLite schema、读写、报告组装、反馈、proposal、Zotero 状态和 LLM usage ledger。
- `internal/config/`：`.env`、本地 settings、secret store、运行路径和配置字段。
- `internal/runtime/`：应用版本、运行模式、路径、进程、打开外部目标、平台差异。
- `internal/trayapp/`：托盘生命周期、本地调度、后端监督、开机自启。
- `internal/zotero/`：Zotero Web API collection 查询和保存论文。
- `web/`：React 前端。应用编排在 `web/src/app/`，功能模块在 `web/src/features/`，API 和生成数据在 `web/src/api/`、`web/src/data/`，共享 UI 和类型在 `web/src/shared/`。
- `tools/`：打包、品牌资产、Linux helper、发布 DNS 更新等脚本。
- `installer/`：Windows 安装包脚本。
- `docs/`：当前手工测试清单、API 文档和维护文档。

## 5. 配置与本地状态

`.env.example` 是推荐配置模板，配置变更时应与 README 和设置 UI 同步。主要配置组：

- 分类器模型：`SCIRSS_CLASSIFIER_API_KEY`、`SCIRSS_CLASSIFIER_BASE_URL`、`SCIRSS_CLASSIFIER_MODEL`、`SCIRSS_CLASSIFIER_THINKING`、`SCIRSS_CLASSIFIER_BATCH_SIZE`；默认关闭 thinking、batch size 为 `5`
- Profile 模型：`SCIRSS_PROFILE_API_KEY`、`SCIRSS_PROFILE_BASE_URL`、`SCIRSS_PROFILE_MODEL`、`SCIRSS_PROFILE_THINKING`
- DeepSeek 计价：Settings → Model 维护 Flash/Pro 的缓存命中、缓存未命中和输出峰谷单价，对应 `SCIRSS_DEEPSEEK_*_CNY_PER_MILLION` 配置；默认值与当前官方人民币价格一致
- Zotero：`SCIRSS_ZOTERO_API_KEY`、`SCIRSS_ZOTERO_LIBRARY_TYPE`、`SCIRSS_ZOTERO_LIBRARY_ID`、`SCIRSS_ZOTERO_COLLECTION_KEY`
- 本地服务：`SCIRSS_SERVER_HOST`、`SCIRSS_SERVER_PORT`

分类器模型只返回 relevance、confidence、简短 reason 和中文标题。`recommended_action` 由 Go 运行时按 relevance 映射，`decision_trace` 不进入模型输出契约。Windows 托盘的新调度设置默认使用本地时间 `12:30`，该时间在中国标准时间下属于 DeepSeek 午间空闲窗口；已有 `tray-settings.json` 中的时间保持不变。

不要提交这些用户本地状态：

- `.env`
- `data/classification_profile.json`
- `data/literature.sqlite`
- `data/rss_feeds.json`
- `data/verification-sessions.json`
- `logs/`
- `reports/`
- `.tmp/`
- `.pnpm-store/`
- `.npm-cache/`

## 6. 维护流程

### 改后端 API

1. 在 `internal/api/` 中确认路由、对应领域 handler、方法、请求体和响应体。
2. 如涉及后台任务，检查 `internal/api/jobs.go` 和 `internal/jobs/`。
3. 更新前端 `web/src/api/client.ts` 和 `web/src/shared/types.ts`。
4. 更新 `docs/api.zh-CN.md`。
5. 添加或调整 `internal/api/*_test.go`。
6. 运行 `go test ./...`，如果前端类型受影响，运行 `corepack pnpm --dir web build`。

### 改同步 pipeline

1. 先确认改动属于抓取、解析、元数据、分类、报告重建还是 job 状态。
2. RSS 抓取和解析优先改 `internal/feeds/`。
3. 同步顺序和进度优先改 `internal/jobs/`。
4. 报告输出性能优先检查 `internal/store/sqlite/` 的 batched query。
5. 不要让 `/api/report/latest` 重新走逐篇查询或重新打开 SQLite。
6. 更新架构文档和必要的 changelog。
7. `/api/admin/run` 对 sync 采用 single-flight：`queued`、`running`、`waiting_for_user` 都是活跃状态，重复触发必须复用已有 job；修改启动流程时应保留注册表内“检查并登记”的原子性。

### 改分类或 Profile

1. `internal/classifier/` 负责分类请求和 prompt shell。
2. `internal/profile/` 负责 Profile schema、生成、校验和 proposal 差异。
3. Profile 文件是用户本地状态，不应提交真实 `data/classification_profile.json`。
4. 如果变更影响用户理解分类结果，更新 README 或维护文档。
5. 如果变更影响 proposal 审阅流程，更新 `ARCHITECTURE.md`。
6. 新增 LLM 调用时显式传递 job collector，并确保成功响应只记录一次且带响应时间；collector 在 job 启动时锁定当前手动价格设置。DeepSeek 高峰价只适用于北京时间周一至周五 9:00–12:00、14:00–18:00，周末和其余时段使用空闲价；已完成 ledger 保存具体费率，后续调价不得回算历史。usage ledger 写入失败只能记录 warning，不能反向让业务 job 失败。

### 改 UI

1. 保持主 UI 三栏布局：左侧过滤、中间列表、右侧详情。
2. 论文卡片保持摘要预览，不要把复杂动作重新塞回卡片。
3. 右侧详情面板负责 `DOI link`、`Mark as read`、`Save to Zotero`、`Mark wrong`。
4. Admin 面板负责订阅、设置、后台任务、反馈和 Profile proposal。
5. 不要让设置、scheduler、proposal、feedback hydration 阻塞论文列表首屏。
6. 在 `web/src/app/App.tsx` 和 `web/src/main.tsx` 中，顶层 helper 和组件保留简短中文职责注释。

### 改 Zotero

1. 后端集成在 `internal/zotero/`。
2. 保存状态写入 SQLite，由报告 API 带回前端。
3. API key 不回显给前端明文。
4. 修改保存逻辑后测试 collection 查询、默认 collection 和手动选择 collection。

### 改受保护订阅验证

1. 默认验证器是 `cmd/feedmedaily-protected-verifier/`。
2. 验证按 host 分组，一次验证会话可覆盖同 host 多个 feed。
3. 失败时支持系统浏览器 fallback 和手动粘贴 RSS XML。
4. 后端 API 在 `/api/feeds/verification/*`。
5. 日志关注 `logs/protected-verifier/` 和 job 日志。

### 改打包或发布

1. 检查 `tools/build_release.ps1`、`installer/feedmedaily.iss`、品牌资产和版本号。
2. 构建前运行前端 build 和 Go tests。
3. 用户可见行为更新写入 `CHANGELOG.md` 当前 unreleased section。
4. release draft 只在发布准备期临时存在，保持短 bullet 风格，发布后删除。
5. 发布 GitHub Release 时上传安装包和 `dist/update.json`，再用 `tools/update_release_dns.ps1` 更新 DNS TXT 记录。

## 7. 排障手册

### 本地服务打不开

1. 确认是 source mode 还是 release mode。
2. Source mode 看 `logs/`，release mode 看 `%LOCALAPPDATA%\FeedMeDaily\logs`。
3. 检查 `SCIRSS_SERVER_HOST` 和 `SCIRSS_SERVER_PORT`。
4. 直接运行后端确认错误：

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

### 前端空白或旧页面

1. 重新构建前端：

```powershell
corepack pnpm --dir web build
```

2. 确认后端 `static_dir` 指向 `web/dist`。
3. 如果 API 正常但页面空白，优先看浏览器控制台和 `web/src/api/client.ts` 对应调用。

### 论文列表加载慢

1. 优先检查 `/api/report/latest`。
2. 不要把 app update、settings、scheduler、proposal 或 feedback 请求作为列表首屏依赖。
3. 检查 SQLite 是否仍为长连接复用，报告是否仍为 batched query。

### pnpm 配置或安装异常

项目真实前端 workflow 是：

```powershell
corepack pnpm --dir web install
corepack pnpm --dir web build
```

如果出现用户级 pnpm rc 权限警告，不要直接判断 pnpm 不可用。先确认命令是否继续执行，再用：

```powershell
corepack pnpm --dir web config get minimum-release-age
corepack pnpm --dir web config get block-exotic-subdeps
```

### Bash helper 在 Linux/WSL 失败

1. 用 Bash 运行，不要直接用 PowerShell 执行 `.sh`。
2. 检查是否 CRLF 导致 `$'\r': command not found`。
3. `.gitattributes` 应保持 `*.sh text eol=lf`。
4. cron 中必须设置正确路径和必要环境变量。

### RSS 抓取被 Cloudflare 或登录页拦截

1. job 会进入 `waiting_for_user`。
2. UI 可打开 native WebView2 验证器。
3. 如果验证器不能捕获 XML，使用 system browser fallback。
4. 用户完成验证后粘贴最终 RSS/Atom/RDF XML。
5. 后端会用同一 parser 校验 XML，校验通过后恢复暂停的 sync。

### Zotero 保存失败

1. 检查 `SCIRSS_ZOTERO_API_KEY`、library type、library ID。
2. collection key 可以为空，前端会展示 collection picker。
3. 错误状态会写入 SQLite 并通过 report 返回。
4. 如果论文没有 classification，保存会失败。

## 8. Git 与发布卫生

- 修改前先看 `git status --short`，不要覆盖不相关本地改动。
- 一个功能、修复或文档范围尽量对应一个聚焦提交。
- 如果用户要求提交，除非另有说明，提交范围可包括用户本地已准备好的相关改动。
- 提交前检查 staged diff，不要提交 secret、本地数据库、日志、缓存或临时文件。
- 行为变化、运行时变化、打包变化、重要 UX 变化应更新 `CHANGELOG.md`。
- 纯计划文档、内部说明和 agent policy 变化通常不需要 changelog。

## 9. 新维护者接手清单

1. 阅读 `README.md`，确认产品定位和 source mode 启动方式。
2. 阅读 `ARCHITECTURE.md`，理解模块边界和数据流。
3. 阅读 `docs/api.zh-CN.md`，理解前后端 API 合约。
4. 复制 `.env.example` 到 `.env`，填写测试用 API key。
5. 运行 `corepack pnpm --dir web install` 和 `corepack pnpm --dir web build`。
6. 运行 `go test ./...`。
7. 用 `go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000` 启动后端。
8. 打开本地 UI，完成 onboarding 或使用已有本地 Profile。
9. 做一次 `Run Sync Now`，观察 job 状态和 `logs/`。
10. 修改任何行为前，先确定变更会落在哪个模块和哪份文档。
