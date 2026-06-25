# FeedMeDaily 中文架构文档

本文描述 FeedMeDaily 当前架构。根目录 `ARCHITECTURE.md` 仍保留为英文架构摘要；本文面向中文维护者，补充运行、维护和排障视角。

## 1. 架构总览

FeedMeDaily 是本地优先应用，由以下部分组成：

- Go 后端服务：提供本地 HTTP API、静态前端、RSS 同步、分类、Profile proposal、Zotero 和受保护订阅验证编排。
- SQLite 数据库：保存论文、分类、反馈、Profile proposal、Zotero 保存状态等运行数据。
- Windows tray：安装版主入口，负责启动和监督后端、打开 UI、触发同步、开机自启和本地定时。
- React/Vite 前端：三栏论文审阅 UI、设置、订阅管理、反馈、Profile proposal review 和 Zotero collection picker。
- LLM Profile：用户兴趣边界和分类规则保存在本地 profile 文件，后端只拥有任务 shell 和输出 schema。
- Zotero Web API：保存选中论文到用户或群组 library。
- 受保护 RSS 验证器：使用 Windows WebView2 或浏览器 fallback 处理 Cloudflare/登录挑战。

高层数据流：

```text
RSS feeds
  -> Go fetch client
  -> RSS/Atom/RDF parser
  -> publisher extractors
  -> SQLite papers
  -> metadata enrichment
  -> LLM classification by profile
  -> SQLite classifications
  -> /api/report/latest
  -> React review UI
  -> feedback/read/Zotero/profile proposal actions
```

## 2. 运行模式和路径

### Source mode

开发命令传入 `--root .` 时进入源码模式。所有运行状态默认在仓库内：

- `data/rss_feeds.json`：RSS 订阅源。
- `data/classification_profile.json`：当前分类 Profile。
- `data/literature.sqlite`：论文、分类、反馈、proposal、Zotero 状态。
- `data/verification-sessions.json`：受保护订阅的 host-level 验证状态。
- `logs/`：后端和 job 日志。
- `logs/protected-verifier/`：受保护订阅验证器日志。
- `tray-settings.json`：source mode 下的托盘调度设置。
- `web/dist/`：前端构建产物，由后端静态服务提供。

### Release mode

安装版以托盘程序为主入口。运行状态在 `%LOCALAPPDATA%\FeedMeDaily\` 下，安装目录只放程序、静态资源和辅助二进制。排障时要区分安装目录和用户数据目录。

## 3. 后端服务

后端入口是 `cmd/feedmedailyd/`。它创建 `internal/api.Server`，挂载本地 API 和静态前端，并持有长期复用的 SQLite store。

后端职责：

- 提供 `/api/app/*` 运行状态和控制接口。
- 提供 settings、feeds、scheduler、profile、feedback、paper read、Zotero、admin jobs、verification API。
- 服务 `web/dist`，并为前端路由提供 SPA fallback。
- 执行同步 pipeline：fetch -> ingest -> enrich -> classify -> report refresh。
- 维护后台 job 状态，供前端轮询。
- 管理受保护 feed 的暂停、验证、XML 注入和恢复。

SQLite 访问分为 read store 和 write store。服务会复用长连接，避免每个请求打开/关闭数据库。`/api/report/latest` 是性能关键路径，必须保持 batched read，不应引入逐篇补查或同步等待非关键接口。

## 4. 同步 Pipeline

同步由 `internal/jobs/RunSync` 负责，主要阶段如下：

1. 读取 `data/rss_feeds.json`。
2. 对每个 feed 发起 HTTP 请求。
3. 检测挑战页、Cloudflare 403、非 feed 内容等异常。
4. 使用通用 parser 解析 RSS/Atom/RDF。
5. 使用 publisher-specific extractor 补充摘要、图片、DOI 等字段。
6. 将论文 upsert 到 SQLite。
7. 对缺失核心字段的论文做元数据补全。
8. 调用分类器模型进行 Profile 驱动分类。
9. 写入 classification。
10. 重建最新报告视图数据，供 `/api/report/latest` 读取。

同步任务通过 job API 暴露状态。job payload 同时包含人类可读 message 和结构化 progress 字段，前端可以显示当前 feed、metadata/classification 百分比和 Profile generation 步骤。

## 5. 模块边界

### `internal/api`

API 层只负责 HTTP 协议、请求校验、响应格式、job 编排和跨模块调用。错误统一返回：

```json
{"detail":"..."}
```

不要把 RSS parsing、classification、Zotero 细节直接堆到 handler 中。handler 应调用对应 domain package。

### `internal/jobs`

任务层负责长流程和后台进度：

- 一次完整同步。
- 指定 scope 重分类。
- 初始 Profile proposal。
- 反馈驱动 Profile proposal。
- report refresh。

任务层通过 progress callback 把阶段进度交回 API job registry。

### `internal/feeds`

负责订阅和 feed 内容：

- `rss_feeds.json` 读写、校验、去重。
- HTTP fetch client 和 retry。
- challenge page 检测。
- RSS/Atom/RDF parser。
- 出版商特殊抽取逻辑。

### `internal/classifier`

负责 LLM 分类：

- 组装分类任务 shell。
- 应用 Profile 规则。
- 控制输出 schema。
- 处理 provider thinking mode 和 fallback。

分类结果包含 relevance、confidence、reason、recommended action、translated title。当前不持久化 paper-level topic tags。

### `internal/profile`

负责 Profile 生命周期：

- 读取和写入当前 Profile。
- 初始 Profile 生成。
- 反馈驱动 proposal 生成。
- proposal changes 差异结构。
- 局部 apply、reject 和版本冲突检查。

Profile 文件中的用户兴趣边界、topic taxonomy、few-shots 和 notes 是分类规则来源。

### `internal/store/sqlite`

负责本地持久化：

- paper upsert 和查询。
- classification 保存和 latest selection。
- feedback 创建、删除、标记 used。
- profile proposal 保存和状态转换。
- Zotero save status。
- report payload 组装。

报告组装应保持批量查询，避免 UI 首屏退化。

### `internal/config`

负责配置解析：

- `.env`。
- 系统环境变量。
- 本地 settings store。
- secret store。
- 默认值。
- source/release 路径解析。

secret 不应以明文回传前端。前端只接收字段是否 configured、来源和可编辑元数据。

### `internal/runtime`

负责跨平台运行辅助：

- app root 和用户数据路径。
- package version。
- process state。
- open external target。
- tray scheduler task name。
- source/release mode。

### `internal/trayapp`

负责 Windows 桌面壳：

- 单实例托盘。
- 后端监督。
- 打开浏览器 UI。
- Run Sync Now。
- launch at login。
- 本地 daily schedule。

### `internal/zotero`

负责 Zotero Web API：

- collection 列表。
- 默认 collection。
- 论文保存。
- 保存错误映射。

## 6. 前端架构

前端位于 `web/`，使用 React、TypeScript、Vite、Tailwind v4、HeroUI v3 beta 和 React Virtuoso。

主要结构：

- `web/src/reportData.ts`：本地 API wrapper。
- `web/src/types.ts`：前端 API 类型。
- `web/src/App.tsx`：主应用状态、布局和主要交互。
- `web/src/components/review/`：三栏审阅相关组件。
- `web/src/components/admin/`：设置、订阅、任务、反馈、Profile proposal 管理。
- `web/src/components/profile/`：Profile 和 proposal review 文档式展示。
- `web/src/components/onboarding/`：首次使用和初始 Profile 生成。
- `web/src/components/modals/`：反馈和 Zotero 保存弹窗。

UI 基线：

- 左侧：过滤器和上下文。
- 中间：虚拟化论文列表，默认 `Unread + Last 30 days`。
- 右侧：论文详情和动作按钮。
- Admin：订阅、设置、任务、反馈、Profile proposal。
- 论文卡片只展示摘要预览，不放复杂动作。
- 关键列表不等待 app update、scheduler、settings、proposal、feedback hydration。
- `Mark as read` 和 feedback 先提交本地 UI 结果，再做非阻塞 report reconcile。

## 7. Profile 生命周期

1. 用户在 onboarding 输入兴趣描述。
2. Profile 模型生成初始 proposal。
3. 用户审阅并可编辑 proposal。
4. 应用 proposal 后写入当前 Profile。
5. 后续分类使用当前 Profile。
6. 用户通过 `Mark wrong` 写入反馈。
7. 后台根据 open feedback 生成新的 full-profile proposal。
8. 用户可接受或拒绝 proposal changes。
9. 应用 proposal 后，只重分类关联反馈论文。

支持的重分类 scope：

- `recent`
- `feedback`
- `all`

## 8. 受保护订阅验证

受保护订阅验证用于处理 Cloudflare、人机验证或登录挑战。架构是 host-scoped session：

1. 同步任务检测到挑战页或 Cloudflare 风格 403。
2. job 进入 `waiting_for_user`。
3. UI 调用 `/api/feeds/verification/start` 打开原生 WebView2 验证器。
4. 验证器使用 `data/verification-profiles/<host>` 下的持久 profile。
5. 同 host 多个 feed 可在同一验证会话中捕获。
6. 验证器捕获 XML 后 POST 到 `/api/feeds/verification/callback`。
7. 后端把 XML 注入正常 feed parser，恢复暂停的 sync。
8. 如果 WebView2 无法捕获，用户可通过 `/api/feeds/verification/browser` 打开系统浏览器。
9. 用户粘贴最终 XML 到 `/api/feeds/verification/manual-submit`。

验证状态保存在 `data/verification-sessions.json`。这让后续 sync 可先尝试已验证 host，失败时再进入 reverify。

## 9. 更新分发

安装版更新检查使用固定 DNS TXT：

```text
feedmedaily-update.stassenger.top
```

TXT 内容格式：

```text
version=<semver>;url=<release-url>
```

`/api/app/update` 会读取 DNS TXT，并根据当前 package version 判断是否有新版本。普通轮询使用短缓存；`?force=1` 用于手动刷新。更新下载和 release notes 仍指向 GitHub Releases。

构建脚本仍会生成 `dist/update.json`。发布 GitHub Release 时需要把它作为 release asset 一起上传，供旧版安装包继续从 `latest/download/update.json` 检查更新。

发布顺序：

1. 更新 changelog。
2. 构建 release。
3. 创建 GitHub Release，并上传安装包与 `dist/update.json`。
4. 更新 DNS TXT。

## 10. 架构维护规则

以下变更必须同步更新架构文档：

- 后端和托盘职责边界变化。
- 同步 pipeline 顺序变化。
- SQLite 数据模型或报告组装策略变化。
- `/api/report/latest` 性能路径变化。
- Profile 生命周期变化。
- UI 三栏职责或 Admin 职责变化。
- 受保护订阅验证流程变化。
- source/release 运行路径变化。
- 更新分发机制变化。

如果只改内部注释、样式微调或测试辅助，通常不需要更新架构文档。
