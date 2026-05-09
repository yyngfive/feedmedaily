# Python/Go 共存期临时应对方案

Status: Draft  
Owner: TBD  
Last Updated: 2026-05-08  
Scope: Windows first / mac compatible / Linux source-first  
Related Docs: `go-backend-migration-master-plan.md`, `go-backend-phase-1-plan.md`

> 本文件只规范迁移过渡期，不替代 `go-backend-migration-master-plan.md`。  
> 本文件用于约束 Python/Go 共存时的职责边界和切换规则。

## Summary

迁移过渡期允许 Python 与 Go 共存，但必须采用“Go 外壳优先，Python 后端逐步下线”的模式，而不是长期双主后端。

目标是：

- 尽快验证 Go 托盘与未来桌面产品形态
- 先验证托盘管理层，再逐步替换后台语言实现
- 允许分阶段替换 Python 业务模块
- 避免出现同一能力长期双生产入口

## Role Definition

### Go

- 负责新托盘程序
- 负责新的服务管理
- 负责未来正式分发形态

### Python

- 在过渡期继续承担未迁移完成的业务逻辑
- 可作为对照实现或临时 fallback
- 不再继续扩展新的桌面分发逻辑

## Transition Stages

### Transition A：Go 托盘 + Python 服务

- 托盘程序由 Go 实现
- 托盘负责：
  - 启动 Python 后端
  - 监控健康状态
  - 执行定时触发
  - 打开浏览器
- Python 继续提供现有 FastAPI `/api/*`
- 该阶段用于尽快验证托盘产品形态是否可行
- 该阶段的重点是“托盘管理层是否成立”，而不是“后台命令是否已经改写为 Go”
- 因此第一阶段允许托盘通过统一命令封装继续执行 Python 命令

### Transition B：Go 服务 + 部分 Python 参考实现

- 核心 API 切到 Go
- Python 保留为：
  - 回归参考实现
  - fixture 生成工具
  - 行为比对工具
- 正式产品运行路径默认只走 Go

### Transition C：移除运行时 Python 依赖

- Windows 正式分发中不再包含 Python 运行时
- Python 仅保留为仓库中的参考或测试辅助代码
- 是否彻底删除 Python 代码，单独后续决定，不作为迁移首版必要条件

## Hard Constraints

- 不允许同一个能力长期同时维护 Python 和 Go 两个“生产入口”
- 每迁移完一个子系统，必须明确：
  - 哪个版本是生产版本
  - 哪个版本只作回归对照
- 前端始终只连接一个后端入口
- 不允许前端同时命中 Python 和 Go 两套服务

## Temporary Release Strategy

- 内部测试版可接受：
  - `Go 托盘 + Python 服务`
- 对外正式版目标：
  - `Go 托盘 + Go 服务`

## Operating Rules During Coexistence

- 托盘、服务管理、自启动、定时能力优先落在 Go 侧
- Python 不再承担新的桌面壳职责
- 如某能力尚未迁移到 Go，允许由 Go 托盘驱动 Python 服务调用该能力
- 托盘侧应尽早抽象出统一命令接口，避免把 Python 调用方式写死在托盘交互层
- 第一阶段的托盘验收不依赖命令语言；只要用户侧行为一致，后端命令可先保持 Python
- Python 仅作为阶段性业务承载，不作为未来正式分发基础

## Exit Condition

满足以下条件后，可进入“运行时移除 Python 依赖”：

- Windows 正式版的后台服务已切换到 Go
- 关键 `/api/*` 已由 Go 完整提供
- 前端在 Go 服务上运行稳定
- 手动任务、定时任务、proposal、Zotero 和数据读取链路均已验证可用
