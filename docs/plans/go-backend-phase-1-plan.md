# Go 后端迁移第一阶段执行计划

Status: Draft  
Owner: TBD  
Last Updated: 2026-05-08  
Scope: Windows first / mac compatible / Linux source-first  
Related Docs: `go-backend-migration-master-plan.md`, `python-go-coexistence-transition.md`

> 本文件受 `go-backend-migration-master-plan.md` 约束。  
> 本文件中的共存规则以 `python-go-coexistence-transition.md` 为准。

## Summary

第一阶段只覆盖“Windows 首发可用、托盘驱动、前端保持兼容”的最小闭环，不包含 mac 发布、Linux 打包或前端大重构。

第一阶段的目标是先把 `Go 托盘管理层` 搭出来并单独验证：

- Go 托盘程序可启动并管理后台服务
- 托盘程序的服务管理、自启动、定时和浏览器打开能力可先独立验收
- 托盘触发的具体后台命令可以暂时是 Python，也可以是后续的 Go
- 前端继续通过浏览器访问本地服务
- 调度和自启动能力先迁移到托盘程序

也就是说，第一阶段优先验证“托盘能不能把整个运行时壳子管起来”，Go 后端重写属于后续阶段目标，而不是第一阶段前置条件。

## In Scope

- 搭建 `feedmedaily-tray` 托盘骨架
- 完成托盘启动服务、打开浏览器、停止服务、查看状态、打开目录
- 完成托盘侧定时执行和开机自启设置
- 完成托盘对现有后台命令的统一封装与触发
- 完成“运行全流程任务”的托盘触发链路
- 为后续 Go 后端替换预留统一命令接口
- 前端不改协议，仅允许调度区块变为兼容说明或后续隐藏
- 首批只支持 Windows

第一阶段允许后台命令继续复用当前 Python 运行路径，只要托盘侧用户体验和运行时控制能力已经成立即可。

## Out Of Scope

- macOS `.app` / `.dmg`
- Linux 安装包
- Linux 托盘
- `cron` 自动写入
- Wails / 内嵌窗口
- 完整的 Go 后端重写
- `feedmedailyd` 成为唯一正式后台服务
- 前端 API 重设计
- 复杂 feed 兼容增强 beyond 现有行为复刻

## Ordered Implementation Sequence

1. 建立 Go 项目骨架与目录结构
2. 实现托盘程序最小可用菜单
3. 实现托盘驱动的启动/停止服务、打开浏览器、打开目录
4. 为托盘建立统一的后台命令封装，先接入现有 Python 命令
5. 实现托盘定时器与自启动设置持久化
6. 验证托盘可稳定触发“运行全流程任务”
7. 收尾前端中的 scheduler 区域兼容处理
8. 在托盘能力稳定后，再搭建 `feedmedailyd` 服务骨架
9. 再逐步迁移 `AppMeta`、healthcheck、静态资源托管
10. 再迁移 config + keyring + 路径解析、SQLite schema + 读写层、job registry 与 admin job 接口
11. 最后进入 metadata / classifier / zotero / profile / pipeline / `feeds.py` 的完整 Go 重写

## Expected Deliverables

### Tray deliverables

- `feedmedaily-tray` 能单实例运行
- 可从托盘打开浏览器
- 可从托盘启动和停止后台服务
- 可从托盘打开数据目录与日志目录
- 可在托盘中设置每日定时执行
- 可在托盘中设置开机自启动
- 可通过统一命令封装触发现有后台全流程任务
- 托盘触发的是 Python 还是 Go 命令，不影响第一阶段交付定义

### Transition deliverables

- 现有后台命令已被托盘统一接管
- 未来将 Python 命令替换为 Go 命令时，不需要重做托盘交互模型
- `feedmedailyd` 可作为后续阶段入口预留，但不是第一阶段交付前提

### UI compatibility deliverables

- 现有前端无需改协议
- 调度区块允许临时显示兼容说明，或在第一阶段末尾隐藏
- 报告、反馈、proposal、Zotero 流程仍保持可用

## Acceptance Criteria

- Windows 安装后可通过托盘打开应用
- 应用运行时无终端窗口弹出
- 托盘可启动、停止并监控当前后台服务
- 托盘可手动触发当前完整运行命令
- 定时到点可自动跑当前完整流程
- 托盘触发链路不要求此时已经换成 Go 后端
- 前端在现有服务上仍可正常工作

## Phase-1 Notes

- 第一阶段实施优先采用 `Transition A`：`Go 托盘 + Python 服务`
- 第一阶段的核心产物是“托盘管理层成立”，不是“Go 后端已完成”
- 当托盘形态和运行时管理稳定后，再切入 Go 服务替换
- `feeds.py` 必须放在最后迁移，以避免过早放大站点兼容风险
