# FeedMeDaily 手工回归清单

这份清单用于验证当前 Go 后端、Windows 托盘、React UI 和安装包。接口字段的完整定义见 [本地 API 文档](./api.zh-CN.md)，这里不重复维护接口契约。

## 1. 测试原则

- 先运行自动检查，再做手工冒烟。
- 先区分 source mode 和 release mode，再判断日志与数据路径。
- 没有对应外部账号时，可以跳过 Zotero、真实 LLM 和受保护 feed，但要在结论中注明。
- 不要提交 `.env`、本地数据库、日志、报告、验证 profile 或真实密钥。

## 2. 环境准备

### 2.1 Source mode

在仓库根目录执行：

```powershell
Copy-Item .env.example .env
corepack pnpm --dir web install
corepack pnpm --dir web build
```

按测试范围填写 `.env`：

- 分类：`SCIRSS_CLASSIFIER_ENABLED_MODELS`、`SCIRSS_CLASSIFIER_DEFAULT_MODEL`、`SCIRSS_DEEPSEEK_API_KEY`、`SCIRSS_GLM_API_KEY`（旧 `SCIRSS_CLASSIFIER_*` 变量只用于迁移）
- Profile：`SCIRSS_PROFILE_API_KEY`、`SCIRSS_PROFILE_BASE_URL`、`SCIRSS_PROFILE_MODEL`
- Zotero：`SCIRSS_ZOTERO_API_KEY`、`SCIRSS_ZOTERO_LIBRARY_TYPE`、`SCIRSS_ZOTERO_LIBRARY_ID`

Source mode 使用仓库内路径：

- 日志：`logs/`
- 用户数据：`data/`
- 数据库：`data/literature.sqlite`
- Profile：`data/classification_profile.json`
- 托盘设置：`tray-settings.json`

### 2.2 Release mode

安装版数据位于：

```text
%LOCALAPPDATA%\FeedMeDaily\
```

安装目录只存程序和静态资源。排查安装版时，不要把仓库内 `logs/`、`data/` 当成实际运行状态。

### 2.3 可跳过项

- 没有 LLM key：跳过 profile generation、sync classification、reclassify。
- 没有 Zotero key：跳过 collection picker 和 save。
- 没有会触发挑战的 feed：跳过 WebView2 与浏览器 fallback。
- 非 Windows：跳过托盘、WebView2 和安装器。

## 3. 自动检查

### 3.1 Go

```powershell
go test ./...
```

预期：全部 package 通过，没有新增失败或 panic。

### 3.2 前端类型检查

```powershell
corepack pnpm --dir web test
```

预期：TypeScript build mode 成功退出。

### 3.3 前端生产构建

```powershell
corepack pnpm --dir web build
```

预期：生成 `web/dist/index.html` 和静态资源，没有 unresolved import。

## 4. 五分钟 Source mode 冒烟

### 4.1 启动后端

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

预期：

- 进程保持运行。
- 输出服务地址与日志位置。
- 浏览器可访问 `http://127.0.0.1:8000`。

### 4.2 探活

在另一个 PowerShell 窗口执行：

```powershell
$base = "http://127.0.0.1:8000"
Invoke-RestMethod "$base/api/app/health"
Invoke-RestMethod "$base/api/app/meta"
Invoke-RestMethod "$base/api/report/latest"
```

预期：

- health 返回 `status = "ok"`。
- meta 的 mode、install/data/logs/static 路径符合 source mode。
- report 即使为空也包含 `totals`、`papers`、`errors`。

### 4.3 打开 UI

访问 `http://127.0.0.1:8000`。

预期：

- 页面不是 404 或空白页。
- 无 Profile 时进入 onboarding。
- 有 Profile 时三栏界面先出现，论文列表不等待 Settings 数据。
- 没有 feeds 时，feeds 加载完成后进入订阅初始化状态并打开 Feeds。

## 5. Windows 托盘

启动：

```powershell
go run .\cmd\feedmedaily-tray --root .
```

检查：

1. 只出现一个托盘实例。
2. 双击或 `Open FeedMeDaily` 打开本地 UI。
3. `Run Sync Now` 能创建后台 job。
4. `Open Data Folder` 和 `Open Logs Folder` 打开 source mode 路径。
5. `Launch at login` 可切换并保持状态。
6. `Enable Daily Sync` 保存时间后，菜单与 Web UI 状态一致。
7. `Quit Tray And Stop Service` 同时退出托盘和后台服务。
8. 启动、打开和退出期间不应闪烁额外命令行窗口。

## 6. Onboarding

前置：暂时移走本地 `data/classification_profile.json`，不要删除唯一副本。

检查：

1. 页面显示基础设置和可展开的高级设置。
2. `Classification models` 多选至少保留一个模型，`Default classifier` 只列出已选模型。
3. 每个模型只显示一行“模型名 + API key + Test connection”；默认模型使用与 Filter 一致的下拉样式，并且只列出已有 key 或当前新填 key 的模型。
4. `Test connection` 进入 `model-test` job，并提示会消耗少量额度；临时 key 不保存。
5. 选择 DeepSeek 且已有 key 时，一次性复用到 Profile 的选项默认勾选；已有 Profile key 不会被覆盖；仅 GLM 时没有 Profile key 不允许生成。
6. `Save Settings` 可以只保存本地设置，不强制生成 Profile。
7. 输入兴趣描述后启动初始 Profile 生成。
8. job 状态从 queued/running 更新到 completed 或 failed。
9. 生成成功后自动出现 proposal review，无需刷新页面。
10. 用户可以编辑初始 Profile 草稿。
11. Accept 后保存当前 Profile 并进入三栏界面。
12. Reject 后 proposal 状态更新，页面仍可继续生成新 proposal。
13. 失败消息只显示一次，不重复出现在多个区域。

## 7. Settings 五个页面

### 7.1 Dashboard

- 显示 runtime、server、data、logs 和版本信息。
- 手动 `Check for updates` 进入 checking 状态并刷新 last checked。
- `Sync` 在没有 feeds 时禁用。
- 运行中的 sync 显示 `Stop sync`；点击后请求立即进入 stopping，最终 job 为 `cancelled`，之后可以再次启动 sync。
- 定向同步只提交当前选中的已保存 feed URL。
- 最新 job 显示阶段、结果数字、warnings 和错误详情。
- LLM job 完成或失败后显示估算费用、缓存命中/未命中和输出 token；非官方 endpoint 或未知模型显示费用不可用。
- `LLM usage · last 3 days` 按完成时间倒序显示 job、模型、请求数、token 和费用，重启后历史记录仍存在。
- 北京时间周一至周五 9:00–12:00、14:00–18:00 使用高峰价，其余时间和周末使用空闲价；跨时段 job 的 pricing snapshot 同时保留两种 tier。

### 7.2 Feeds

- 目录可按出版社和关键字筛选。
- 已存在的 URL 不可重复添加。
- 手动新增必须同时填写名称和 URL。
- 已保存 feeds 默认紧凑只读；进入 Edit 后可以修改和删除。
- 保存后刷新页面，订阅顺序和内容保持。

### 7.3 Profile

- 当前规则按 direct、indirect、unrelated 分区显示和编辑。
- 保存后 Profile 版本和内容刷新。
- open feedback 表格显示原分类、修正分类、note 和删除操作。
- pending proposal 可以逐条接受/拒绝后应用，也可以整体拒绝。

### 7.4 Model

- `Classification models` 多选、按模型 key、启用/停用和默认模型联动正确；默认值不能脱离启用集合。
- DeepSeek V4 Flash 请求使用 `thinking=disabled` 且无 `reasoning_effort`；GLM-5.3-Flash 请求使用 `thinking=enabled` + `reasoning_effort=low`，失败重试不发送 disabled-thinking。
- `Test connection` 以后台 `model-test` job 运行，成功/失败状态可轮询，且日志不出现 key。
- secret 值不回显明文。
- environment override 状态清楚显示。
- 停用模型保留 key；只有明确 `Clear key` 后才删除。
- 保存后同一进程内启动的新 job 使用新设置。
- Token pricing 表默认显示当前 DeepSeek Flash/Pro 峰谷价格和 GLM-5.3-Flash 限时价格，允许分别编辑缓存命中、缓存未命中和输出单价。
- 保存定价后，新启动的 job 使用新价格；保存前已完成或正在运行的 job 继续显示原价格快照，不被回算。

### 7.5 App

- Zotero 与本地应用字段分组正确。
- daily sync 时间可启用、更新和删除。
- 非 Windows 平台显示自动调度不可用提示，不伪装成成功安装任务。

## 8. 论文审阅

至少准备两篇不同期刊、日期和 relevance 的论文。

检查：

1. 默认筛选为 `Unread + Last 30 days`。
2. 关键字、日期、relevance、read、feedback、期刊多选可组合。
3. 日期、期刊、置信度排序结果稳定。
4. 点击卡片后右侧详情与选择同步。
5. 长作者列表单独滚动，不挤掉摘要和按钮。
6. DOI link 指向正确地址。
7. Mark as read 后论文立即从 Unread 消失，选择移动到合理位置。
8. Mark as unread 后状态恢复。
9. Mark selected and above read 只影响当前可见范围。
10. Mark all visible read 只影响当前筛选结果。
11. 后台 report reconcile 不应清空列表或把选择跳回旧论文。

## 9. Feedback 与 Profile proposal

检查：

1. 在详情面板打开 `Mark wrong`。
2. 选择新 relevance，填写 note 并提交。
3. 卡片和详情立即显示 feedback 状态。
4. Profile 页出现对应 open feedback。
5. 删除 feedback 后卡片状态和列表同步清除。
6. Generate proposal 创建后台 job。
7. proposal 完成后自动刷新 review 文档。
8. Apply 只重分类该 proposal 关联的 feedback papers。
9. Reject 保留原 feedback，不误标 used。
10. `recent`、`feedback`、`all` 三种 reclassify scope 均可启动。

## 10. Zotero（可选）

前置：使用测试库或确认允许写入的 collection。

检查：

1. 打开 `Save to Zotero` 后异步加载 collections。
2. 大型库可以继续翻页直到加载完整。
3. collection 层级缩进正确。
4. 不选 collection 时按当前默认语义保存。
5. 选择 collection 后保存到正确位置。
6. 成功后按钮显示 Saved，刷新页面后状态保持。
7. 作者、日期、DOI、URL 和摘要元数据正确。
8. API 错误写入本地状态并显示可理解的错误。

## 11. 受保护 Feed（可选，Windows）

使用允许测试且会触发挑战的 RSS 地址。

检查：

1. sync 检测挑战后 job 进入 `waiting_for_user`。
2. Dashboard 显示 feed 名称和验证操作。
3. 原生 WebView2 verifier 使用 host-scoped profile。
4. 同 host 多个 feeds 不重复创建无意义的新验证会话。
5. 捕获 RSS/Atom/RDF 后，原 job 继续而不是从头抓取。
6. 原生 verifier 失败时可打开系统浏览器。
7. 粘贴空值、HTML 或错误 feed XML 会被拒绝。
8. 粘贴有效 XML 后 job 恢复。
9. 超时或退出后不会遗留无限增长的 verifier 进程。
10. 诊断写入 `logs/protected-verifier/`。

## 12. Release 构建

### 12.1 仅构建 release 目录

```powershell
pwsh -File .\tools\build_release.ps1 -SkipInstaller -SkipFeedCatalogUpdate
```

检查：

- `dist/FeedMeDaily/FeedMeDailyTray.exe`
- `dist/FeedMeDaily/feedmedailyd.exe`
- `dist/FeedMeDaily/FeedMeDailyProtectedVerifier/FeedMeDailyProtectedVerifier.exe`
- `dist/FeedMeDaily/web/dist/index.html`
- `dist/update.json`
- `update.json` 版本与 `web/package.json` 一致

### 12.2 完整安装器

```powershell
pwsh -File .\tools\build_release.ps1 -SkipFeedCatalogUpdate
```

预期：`dist/installer/FeedMeDaily-v*.exe` 存在且大小大于零。

发布时才执行 feed catalog 网络更新和 DNS TXT 更新；普通回归不要改外部状态。

## 13. 安装、覆盖和卸载

### 13.1 新安装

1. 双击当前 `FeedMeDaily-v*.exe`。
2. 完成安装并选择启动托盘。
3. 检查开始菜单和可选桌面快捷方式。
4. 从托盘打开 UI。
5. 调用 `/api/app/health` 和 `/api/app/meta`。
6. 确认 install dir 指向安装目录，data/logs 指向 `%LOCALAPPDATA%\FeedMeDaily`。
7. 保存一条 feed 和 scheduler 设置，重启后仍存在。

### 13.2 覆盖安装

1. 保留现有用户数据。
2. 用同版本或待发布新版本覆盖安装到同一路径。
3. 确认安装目录不残留旧 Python 运行壳或 `_internal`。
4. 确认托盘、后台、UI 和已有设置仍正常。

### 13.3 卸载

1. 从 Windows 应用列表卸载 FeedMeDaily。
2. 确认安装目录、开始菜单和桌面快捷方式被移除。
3. 用户数据目录是否保留按当前产品策略记录，不手工删除测试者真实数据。

## 14. 常见失败定位

### 页面打不开

- 检查 `/api/app/health`。
- 检查当前运行模式对应的日志目录。
- 检查端口是否被其他进程占用。

### 页面空白或静态资源旧

- 重新运行 `corepack pnpm --dir web build`。
- 检查 app meta 的 static dir。
- 查看浏览器控制台和 `web/src/api/client.ts` 对应请求。

### Sync 或 reclassify 失败

- 检查 classifier 配置和 provider 可访问性。
- 检查 feed URL、job warnings 和后端日志。
- 不要用页面刷新掩盖仍在运行的 job。

### Profile 操作失败

- 检查 profile model 配置。
- Generate proposal 需要当前 Profile；反馈驱动更新还需要 open feedback。
- 查看 proposal job 的 error 和 validation 结果。

### Zotero 失败

- 检查 key、library type、library ID。
- 确认目标论文已有 classification。
- 查看本地 Zotero status 和 API 返回 detail。

## 15. 回归结论模板

```text
版本/分支：
测试日期：
测试环境：source / release，Windows / Linux / macOS

自动检查：
1. go test ./...：通过 / 失败
2. frontend test：通过 / 失败
3. frontend build：通过 / 失败

主流程：
4. backend / tray 启动：通过 / 失败 / 跳过
5. onboarding：通过 / 失败 / 跳过
6. Settings 五页：通过 / 失败
7. feeds 保存与 sync：通过 / 失败 / 跳过
8. 筛选、选择和已读操作：通过 / 失败
9. feedback / proposal / reclassify：通过 / 失败 / 跳过
10. Zotero：通过 / 失败 / 跳过
11. protected feed：通过 / 失败 / 跳过

发布验证：
12. release build：通过 / 失败 / 跳过
13. installer：通过 / 失败 / 跳过
14. 安装、覆盖、卸载：通过 / 失败 / 跳过

失败详情与日志路径：
```
