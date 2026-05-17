# FeedMeDaily v0.2.0 更新说明（草稿）

发布日期：2026-05-17

本版本主要完成了 Python到Go 后端的迁移，修复了部分托盘程序和UI BUG。


- 修复托盘双击打开 UI、网页刷新时命令行窗口闪烁的问题。
- 修复 onboarding 生成初始 profile 成功后，页面不自动刷新显示新 proposal 的问题。
- 修复页面刷新后，顶部消息提示不会继续显示当前进行中 job 状态的问题。
- 改进 `feedmedailyd` 的命令行启动体验，常驻模式现在会输出启动地址、日志目录和停止提示。
- Windows 下通过 `go run ./cmd/feedmedailyd` 启动服务时，现在会尝试自动拉起托盘。
- 修复了部分RSS地址解析失败的问题
