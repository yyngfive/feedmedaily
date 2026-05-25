
版本/分支：codex/go-feedmedailyd-skeleton

测试日期：2026-05-16

测试环境：source and release mode，Windows

1. 通过托盘打开服务会打开命令行，关闭命令行后后台服务也被关闭
2. Web UI的feed list输入框的URL输入框每次输入一个字母就会失去焦点
3. 需要一个比之前更加详细的日志系统，现在不对吧每个运行的命令、成功提示、错误信息写入日志
4. 上次的错误消息在网页刷新后还会被重新提示
5. cmd/feedmedailyd使用终端运行后没有任何信息输出
6. 旧的独立 report rebuild 命令没有生成你说的 `reports/data/latest.json` 和 `reports/latest/index.html`。现在这部分已经确认是历史残留，当前主流程是纯 SQLite 读模型。
7. 之前直接把 `feedmedailyd` 当命令式 sync 入口时没有任何进度提示。当前已经收口成 daemon-only 设计，sync 统一改走后台 job。
8. 之前的python版本添加elsevier来源的rss url后fetch后会报错list index out of range，现在go版本不会报错误了。之前的python版本无法获取nar的文献，现在的go版本没有问题了
9. 没找到你说的D:/Codes/Projects/SciRSSAgent/tray-settings.json文件
10. 网页图标和readme banner还是旧的logo
11. 初始化界面输入偏好后报错，并且会出现两个消息提示条，页面上方的提示条5s后消失，按钮上方的会保持
12. ![1778939606346](image/test-feedback/1778939606346.png)
13. 安装后程序读取的current version是0.0.0，但安装包名称是0.1.3
14. 打开tray settings没反应
15. 从托盘程序点击open会闪过一个命令行
16. 安装后旧的主程序还在，卸载后旧python的tray-settings.json还在![1778940498710](image/test-feedback/1778940498710.png)
