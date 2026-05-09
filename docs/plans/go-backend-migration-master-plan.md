# Go 后端迁移总方案

Status: Draft  
Owner: TBD  
Last Updated: 2026-05-08  
Scope: Windows first / mac compatible / Linux source-first  
Related Docs: `go-backend-phase-1-plan.md`, `python-go-coexistence-transition.md`

## Summary

本方案是 FeedMeDaily 从 `Python + FastAPI + 本地浏览器前端` 迁移到 `Go 后端 + 托盘 + 外部浏览器前端` 的唯一权威总方案。

迁移后的目标形态是：

- `feedmedailyd` 负责本地后台服务、静态资源托管和全部业务逻辑
- `feedmedaily-tray` 负责服务管理、托盘交互、浏览器打开、状态检查、自启动与定时任务
- React 前端继续通过默认浏览器访问本地 `http://127.0.0.1:<port>`
- 迁移首版中前端无需重构即可继续运行

迁移策略分成两个清晰阶段：

- 第一阶段先落地 `Go 托盘管理层`，优先验证托盘程序的单实例、服务管理、浏览器打开、自启动与定时能力
- 第二阶段再逐步完成 `Go 后端重写`，把当前 Python 服务替换为 Go 服务

第一阶段的验收重点是“托盘程序是否能稳定管理后台任务与启动体验”，而不是“托盘触发的具体命令是 Go 还是 Python”。

本文件不写过细的每日实施步骤，不包含具体待办序列号。所有阶段文档必须以本文档为上位依据。

## Locked Decisions

- Windows 是首发正式支持平台
- macOS 保留托盘兼容设计，但不纳入首批验收
- Linux 源码优先，不做托盘，不首发安装包
- 调度由托盘负责，服务本身不做自启动和定时
- 迁移首版保持现有 `/api/*` 协议不变
- 运行时最终目标是不再依赖 Python 解释器和 PyInstaller
- 第一阶段允许托盘先调度和管理 Python 后端命令，后续再替换为 Go 后端命令

## Target Architecture

### Runtime shape

- `cmd/feedmedailyd`
  本地后台服务。负责托管前端静态资源、提供现有 `/api/*` 接口、执行 RSS 抓取、metadata、分类、report、proposal 和 Zotero 相关逻辑。
- `cmd/feedmedaily-tray`
  托盘程序。负责单实例、启动/停止服务、打开浏览器、打开目录、查询状态、自启动与定时触发。

### Go module structure

- `cmd/feedmedailyd`
- `cmd/feedmedaily-tray`
- `internal/api`
- `internal/config`
- `internal/store/sqlite`
- `internal/feeds`
- `internal/metadata`
- `internal/classifier`
- `internal/profile`
- `internal/zotero`
- `internal/jobs`
- `internal/runtime`
- `internal/scheduler`

## Python To Go Migration Mapping

### Direct module replacements

- `server.py` -> `internal/api` + `cmd/feedmedailyd`
- `runtime.py` -> `internal/runtime`
- `config.py` -> `internal/config`
- `secure_store.py` -> `internal/config` secret/keyring layer
- `storage.py` -> `internal/store/sqlite`
- `metadata.py` -> `internal/metadata`
- `classifier.py` -> `internal/classifier`
- `services.py` -> `internal/profile` + `internal/zotero`
- `pipeline.py` -> `internal/jobs` + orchestration layer
- `feeds.py` -> `internal/feeds`

### Migration order

优先迁移稳定边界，再迁移复杂业务链路：

1. 服务骨架与 API 兼容层
2. 配置层、密钥层、路径解析
3. SQLite schema 与读写层
4. job registry 与后台任务接口
5. metadata / classifier / zotero / profile / pipeline
6. `feeds.py`

`feeds.py` 是最高风险模块。首版目标是复刻现有行为，而不是借由新库重写语义。

## API Compatibility

迁移首版必须直接兼容当前前端依赖的 `/api/*` 接口：

- `/api/report/latest`
- `/api/app/meta`
- `/api/app/update`
- `/api/app/open`
- `/api/app/exit`
- `/api/settings/feeds`
- `/api/settings/config`
- `/api/settings/scheduler`
- `/api/profile/current`
- `/api/profile/bootstrap`
- `/api/feedback`
- `/api/papers/{id}/read`
- `/api/profile/proposals`
- `/api/profile/proposals/{id}`
- `/api/profile/proposals/generate`
- `/api/profile/proposals/{id}/apply`
- `/api/profile/proposals/{id}/reject`
- `/api/zotero/collections`
- `/api/zotero/save/{paper_id}`
- `/api/admin/run`
- `/api/admin/reclassify`
- `/api/admin/report/latest`
- `/api/admin/jobs/{id}`

兼容要求：

- 路径、HTTP 方法、请求 JSON、响应 JSON 字段保持不变
- 错误响应继续提供 `detail`
- 现有前端 `web/src/reportData.ts` 在迁移首版中无需改协议

## Packaging And Platform Strategy

### Windows

- 正式首发平台
- 提供托盘和安装器
- 运行形态为 `feedmedaily-tray.exe + feedmedailyd.exe + web 静态资源`
- 应用运行时无终端窗口弹出
- 第一阶段允许托盘先调用现有 Python 后端命令，以验证托盘产品形态
- 第二阶段再将正式分发的后台服务切换为 Go

### macOS

- 架构兼容保留
- 不纳入首批正式交付和验收
- 后续可在同一托盘抽象上补齐 `.app`、签名与公证

### Linux

- 首批仅支持源码运行或直接运行 Go 二进制
- 不做托盘
- 不首发安装包
- 后续如需定时执行，由用户使用 `cron`

## Acceptance Criteria

本总方案的最终验收标准是：

- Windows 正式分发版运行时不再依赖 Python 解释器和 PyInstaller
- 前端在迁移首版中无需重构即可正常运行
- 本地既有 `literature.sqlite`、`rss_feeds.json`、`classification_profile.json` 能被继续读取
- Windows 安装后可通过托盘打开应用
- 托盘形态可驱动后台服务和定时任务
- 抓取、分类、报告、反馈、proposal、Zotero 工作流保持可用

阶段性说明：

- 第一阶段验收聚焦托盘管理层和运行时体验，不要求后台命令已经全部改为 Go
- 总体验收仍以“Windows 正式分发版运行时不再依赖 Python”作为最终完成标准

## Document Relationship

- 本文件是顶层权威文档
- `go-backend-phase-1-plan.md` 受本文件约束，负责定义首批最小闭环实施范围
- `python-go-coexistence-transition.md` 受本文件约束，负责定义迁移过渡期的双栈共存规则
