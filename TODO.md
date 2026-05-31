# TODO

当前较正式的实施路线见 [docs/plans/current-roadmap.md](/D:/Codes/Projects/SciRSSAgent/docs/plans/current-roadmap.md)。本文件保留为快速 backlog 和记事清单。
以下勾选状态按 `main` 分支当前实现校对；能明确从代码与已合并提交确认完成的项已标记为已完成，其余继续保留为未完成。

# Front end

## Profile

- [X] 增加自己编辑Profile功能
- [X] 增加AI自动简化Profile的功能，优化Profile更新逻辑

## Feed

- [ ] 增加一个内置的Feed List以及官方RSS List跳转页
- [ ] 增加获取feed、metadata、llm分类时的具体进度条（当前已有状态消息提示，但还不是进度条）

## Initialization & Settings

- [ ] 精简参数界面，把确认按钮放在下方
- [ ] 增加DeepSeek开发者中心链接指引
- [ ] 增加起始界面的说明文档
- [ ] thinking 自动降级时补更明确的前端提示和设置说明

## Filter

- [ ] 增加mark wrong筛选
- [ ] 增加期刊的多选filter

## Card List

- [ ] 增加按照期刊、日期、置信度的排序功能
- [ ] 增加一个一键全部read功能

## Zotero

- [ ] 用户仓库文件夹获取不全，且有的有有的没有文件夹层级分类

## Update

- [X] 增加内置的manifest

# Server

## Performance

- [ ] [High] 每次打开应用读取数据库都要等待，且会随文献数量增长越来越慢；需要优化启动时/首页加载时的 SQLite 读取与 report 构建路径

## Feed

- [X] APSB等Elsevier期刊解析报错
- [X] NAR的RSS无法获取
- [ ] 避免数据更新时的终端弹窗
- [ ] 托盘启动和双击打开应用时仍会闪烁命令行窗口
- [X] Last Update没有更新却自动增加

## Scheduler

- [ ] 增加对linux和mac的支持

## Installer

- [ ] 增加对mac的支持，linux推荐使用源码安装uv直接运行

## 托盘

- [X] 后台服务检测到托盘程序未启动，自动打开托盘
