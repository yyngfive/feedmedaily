# Front end

## Profile

- [ ] 增加自己编辑Profile功能
- [ ] 增加AI自动简化Profile的功能，优化Profile更新逻辑
- [ ] 如果要恢复 paper-level topic tags，需要先重新设计 tag 的生成、存储和展示方案

## Feed

- [ ] 增加一个内置的Feed List以及官方RSS List跳转页
- [ ] 增加获取feed、metadata、llm分类时的具体进度条
- [ ] onboarding 成功 generate 后当前页面不会自动刷新出新的 profile
- [ ] 页面刷新后看不到仍在运行中的 job 状态

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

- [ ] 增加内置的manifest

# Server

## Feed

- [ ] APSB等Elsevier期刊解析报错
- [ ] NAR的RSS无法获取
- [ ] 避免数据更新时的终端弹窗
- [ ] 托盘启动和双击打开应用时仍会闪烁命令行窗口
- [ ] Last Update没有更新却自动增加

## Scheduler

- [ ] 增加对linux和mac的支持

## Installer

- [ ] 增加对mac的支持，linux推荐使用源码安装uv直接运行

## 托盘

- [ ] 后台服务检测到托盘程序未启动，自动打开托盘
