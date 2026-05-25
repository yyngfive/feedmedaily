# FeedMeDaily v0.3.0

- 新增 Windows 专用的 protected-feed verifier 窗口，受保护 RSS 现在可以在同一次 sync 中完成验证并自动恢复
- 增强 RSS 抓取、解析和 metadata enrichment，修复 Nature RDF、Elsevier/ScienceDirect、bioRxiv 等多类兼容问题
- 统一 sync 入口为 daemon 后台 job，Web UI、tray 和 Linux source mode 脚本现在共享同一套同步状态
- 新增 Linux source mode 辅助脚本，可用于 `serve`、`open`、`sync` 和配合 `cron` 做定时同步
