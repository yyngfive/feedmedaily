# TODO

当前较正式的实施路线见 [docs/plans/current-roadmap.md](/D:/Codes/Projects/SciRSSAgent/docs/plans/current-roadmap.md)。本文件保留为快速 backlog 和记事清单。
以下勾选状态按 `main` 分支当前实现校对；能明确从代码与已合并提交确认完成的项已标记为已完成，其余继续保留为未完成。

# Front end

## UI

- [X] open logs、data、 install按钮失效
- [X] 设置页面滚动问题

## Profile

- [X] 增加自己编辑Profile功能
- [X] 增加AI自动简化Profile的功能，优化Profile更新逻辑
- [X] 更新profile后feedback文章的分类还是保持之前
- [X] 拒绝的Feedback应该保留
- [X] 合并有些过头，导致每个条目很长，不是真正的抽象，而是罗列新的更大的范围，降低了分类准确性

## Feed

- [X] 增加一个内置的Feed List以及官方RSS List跳转页
- [ ] PNAS官方区分了学科
- [X] 增加获取feed、metadata、llm分类时的具体进度条（当前已有状态消息提示，但还不是进度条）

## Initialization & Settings

- [X] 精简参数界面，把确认按钮放在下方
- [X] 增加DeepSeek开发者中心链接指引
- [X] 增加起始界面的说明文档
- [X] 设置抽屉拆分为 Dashboard / Feeds / Profile / Model / App

## Filter

- [X] 增加mark wrong筛选
- [X] 增加期刊的多选filter

## Card List

- [X] 增加按照期刊、日期、置信度的排序功能
- [X] 增加一个一键全部read功能
- [ ] read可以变为unread
- [ ] 可以一键read当前选中文章以及列表之前文章

## Zotero

- [ ] 用户仓库文件夹获取不全，且有的有有的没有文件夹层级分类

## Update

- [X] 增加内置的manifest
- [X] 增加手动检查更新入口（`Settings` + 页面状态栏）

# Server

- [ ] 貌似gemini api不兼容think设置

## Performance

- [X] [High] API Server 复用长期存活的 SQLite Store，不再为每个请求重复 `Open` / `Ping` / `loadColumns`
- [X] [High] `/api/report/latest` 改为批量查询最新 classification / feedback / zotero 状态，并补齐匹配索引以消除 N+1 读取
- [X] [High] 首页 card list 不再等待 app update / scheduler / settings / proposals / feedback 请求；更新检查已移出首屏关键路径并增加短期缓存
- [X] [Medium] 如果大库下首页仍有明显压力，继续评估 report 列表/详情拆分或服务端分页与筛选

## Feed

- [X] APSB等Elsevier期刊解析报错
- [X] NAR的RSS无法获取
- [X] 避免数据更新时的终端弹窗
- [X] 托盘启动和双击打开应用时仍会闪烁命令行窗口
- [X] Last Update没有更新却自动增加
- [X] 2026-06-03 13:21:51,951 WARNING Feed returned unsupported XML root "html" attempt=0 root=html url=https://www.pnas.org/action/showfeed?type=searchTopic&taxonomyCode=topic&tagCode=bio-sci

## Scheduler

- [ ] 增加对linux和mac的支持

## Installer

- [ ] 增加对mac的支持，linux推荐使用源码安装

## 托盘

- [X] 后台服务检测到托盘程序未启动，自动打开托盘
- [ ] Web UI 修改自动同步时间后，运行中的托盘菜单和自动同步逻辑仍未同步到新设置
