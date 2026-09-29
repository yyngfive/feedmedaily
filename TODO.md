# TODO

这里只保留尚未完成的工作；已发布内容见 `CHANGELOG.md`。

## Feeds

- [X] 补充 PNAS 官方学科订阅。
- [X] 补充 APS 订阅源（通过 sci-rss-list 项目）。
- [X] 订阅源增加 Current Issue 和 ASAP 标注（通过 sci-rss-list 项目）。
- [ ] 核实并修正 sci-rss-list 中 ChemRxiv 最新预印本 feed 的范围（当前标为 `single_journal`，应使用 `platform_collection`）。

## Models

- [X] 验证并修复 Gemini API 的 thinking 参数兼容性。

## Performance

- [X] 在真实大库上测量报告载荷；只有现有批量查询仍不足时，再评估列表/详情拆分或服务端分页与筛选。

## Paper Filter

- [X] 期刊筛选界面高度增加
- [X] 期刊筛选界面显示真正的期刊名，而不是带有期号或其他标注的RSS标题

## 分类

- [X] 支持自定义重分类范围，按照期刊或者日期筛选
- [ ] 支持导出一份带有现有profile以及基础约束的prompt，以供外部模型生成优化的格式化profile文件
- [ ] 支持用户导入文章，并加入profile feedback作为示例参考

## 同步

- [X] 按照期刊同步功能缺少搜索框清除按钮
- [X] 按照期刊同步中，选中的期刊缺少已经选中的清晰列表
- [X] nar、chemrxiv等期刊容易出现no such host问题（根因为系统级 DNS 瞬断；已改为同步收尾自动补抓瞬断失败的 feed，保持官方 RSS 源不变）

## 数据库

- [ ] 允许删除特定期刊的全部数据
- [X] 允许手动创建数据库备份
- [ ] cleanup功能不限制在未分类文章而是整个数据库
- [X] cleanup的stop按钮与开始运行按钮并排显示
