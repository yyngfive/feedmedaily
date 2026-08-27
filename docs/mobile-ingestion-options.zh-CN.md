# 手机版采集方案调研：RSS、邮件提醒与学术元数据 API

> 调研快照：2026-08-11。维护状态取自各项目 GitHub 仓库/API 当日页面；许可证以仓库声明为准。

## 结论

手机版不应直接抓出版社 RSS，也不应因为部分 RSS 触发人机验证就整体改成邮件。

最小且稳妥的路线是：**保留 RSS 为主入口，把采集、分类、去重和密钥留在持续在线的 Go 后端；只给实际失败的出版社增加邮件 update alert；再用 Crossref/OpenAlex（生命科学可加 Europe PMC）做补漏。手机只读写 SciRSSAgent API。**

当前项目已经有主机级 WebView2 验证、XML 回灌和同主机复用，但文档也明确承认部分站点更信任完整系统浏览器。因此这条链路适合作为桌面人工恢复手段，不适合作为移动端后台采集基础。[现有架构说明](../ARCHITECTURE.md#protected-feed-verification)

## 为什么不是三选一

| 来源 | 优点 | 固有限制 | 在本项目中的位置 |
| --- | --- | --- | --- |
| 出版社 RSS/Atom | 结构化、增量成本低、与现有解析器完全兼容 | 个别站点加挑战；源可能停更或字段残缺 | 默认主入口 |
| 出版社邮件 update alert | 由出版社主动投递，绕开“抓 RSS 页面”这一步；常含期刊/专题信息 | HTML 模板会变、可能延迟/合并发送、带追踪、确认订阅与退订流程各异 | 仅替代已证实失败的 feed |
| Crossref/OpenAlex/Europe PMC | 不访问出版社页面，可按期刊/日期/主题增量发现 | 不是出版社实时通知；元数据和摘要完整性不一 | 补漏、校验，不替代所有 RSS |

真正的移动化前置条件是：后端必须持续在线，并通过 HTTPS、认证和最小权限 API 供手机访问。若后端仍只在 Windows 本机运行，换成邮件也不会自动得到可靠的手机同步。

## GitHub / 开源方案

### 1. RSS 阅读器、代理和 feed 生成器

| 项目 | 维护/许可快照 | 能解决什么 | 不能解决什么 | 判断 |
| --- | --- | --- | --- | --- |
| [RSSHub](https://github.com/DIYgod/RSSHub) | 2026-08-09 仍有推送；AGPL-3.0（[GitHub API](https://api.github.com/repos/DIYgod/RSSHub)） | 大量站点路由、代理、缓存，也可配浏览器渲染 | 仍受验证码、IP 信誉和页面改版影响；部署一套 TypeScript 服务会增加运维 | 可作为少数无 feed 站点的外部适配器，不应嵌进 Go 主进程 |
| [RSS-Bridge](https://github.com/RSS-Bridge/rss-bridge) | 2026-08-06 仍有推送；Unlicense；官方列出 447 个 bridge、CSS/XPath 与缓存（[README](https://github.com/RSS-Bridge/rss-bridge#readme)） | 把没有 feed 的普通网页转成 RSS/Atom/JSON | 项目发布记录曾因 Cloudflare 问题移除 Binance 公告抓取，说明它不是反验证保证（[release](https://github.com/RSS-Bridge/rss-bridge/releases)） | 与 RSSHub 同类，按站点试用，不作核心依赖 |
| [changedetection.io](https://github.com/dgtlmoon/changedetection.io) | 活跃；Apache-2.0；约 33k stars（[README](https://github.com/dgtlmoon/changedetection.io#readme)） | Playwright/WebDriver、登录步骤、CSS/XPath、代理，并能从网页变化生成 RSS | 浏览器运维重；页面改版和验证码仍会破坏流程；变化事件不等于规范论文记录 | 只适合极少数无 RSS、无 alert、无元数据 API 的关键页面 |
| [Miniflux](https://github.com/miniflux/v2) | 2026-08-09 仍有推送；Apache-2.0（[GitHub API](https://api.github.com/repos/miniflux/v2)） | Go 后端、定时刷新、代理/自定义 UA、稳定 API；是“服务端采集、手机同步”的成熟参考（[API](https://miniflux.app/docs/api.html)） | 代理、UA 和 scraper rule 不会可靠通过人类验证 | 借鉴部署/API 边界即可，不需要替换 SciRSSAgent |
| [FreshRSS](https://github.com/FreshRSS/FreshRSS) | 2026-08-09 仍有推送；AGPL-3.0（[GitHub API](https://api.github.com/repos/FreshRSS/FreshRSS)） | 自托管聚合、网页抓取生成 feed、移动客户端兼容 | 同样不能保证越过挑战；PHP 应用会与现有产品功能重叠 | 不建议引入，除非决定把“阅读器”整体外包 |

结论：这些工具能改善抓取、代理和站点适配，**不能把人机验证变成稳定机器接口**。不要把无头浏览器或验证码绕过作为产品核心能力。

### 2. 邮件提醒转 feed / 邮件直接接入

| 项目 | 维护/许可快照 | 能解决什么 | 不能解决什么 | 判断 |
| --- | --- | --- | --- | --- |
| [Kill the Newsletter](https://github.com/leafac/kill-the-newsletter) | 2026-07-31 仍有推送；MIT（[GitHub API](https://api.github.com/repos/leafac/kill-the-newsletter)） | 为订阅生成收件地址，并把邮件转换为 Atom；可直接喂给 SciRSSAgent 现有 feed 解析器 | feed URL 是访问凭据；依赖 SMTP/DNS 和该服务持续在线；邮件模板质量由出版社决定 | **最适合零产品代码试验** |
| [kill-the-news](https://github.com/juherr/kill-the-news) | 2026-07-24 仍有推送；MIT；新项目、规模小（[README](https://github.com/juherr/kill-the-news#readme)） | 自有域名 + Cloudflare Email Routing；入站地址与私有 feed ID 分离；RSS/Atom/JSON；确认链接提示和发件人白名单 | 绑定 Cloudflare，成熟度低于 Kill the Newsletter | 更重视私有域名和 feed 隔离时可试验，不宜现在直接绑定产品 |
| [go-imap v2](https://github.com/emersion/go-imap) + [go-message](https://github.com/emersion/go-message) | MIT；go-imap 2026-07-02 有推送，但 v2 README 仍标注开发中；go-message 最近推送为 2025-02（对应 [API](https://api.github.com/repos/emersion/go-imap)、[API](https://api.github.com/repos/emersion/go-message)） | Go 后端直接读专用 IMAP 文件夹并解析 MIME，不需要邮件转 feed 服务 | 需要处理 OAuth/应用密码、UID checkpoint、MIME、HTML 清洗、退订/确认和供应商差异 | 只有邮件通道经过试用证明值得产品化后再做 |
| [Open Trashmail](https://github.com/HaschekSolutions/opentrashmail) | Apache-2.0；仓库提供 SMTP、每地址 RSS、JSON API 和 webhook（[README](https://github.com/HaschekSolutions/opentrashmail#readme)） | 一站式接收邮件并输出 RSS/API | 接近完整邮件服务器，攻击面与运维成本明显大于本需求 | 不推荐；为 update alert 自建邮件服务器过度了 |

最懒且可逆的验证方式：先在独立的 Kill the Newsletter/kill-the-news 实例创建一个地址，把其 Atom URL 当普通 RSS 加进当前订阅。这样可以用现有分类、去重和 UI 测试真实邮件质量，而无需先设计“通用邮件采集框架”。

### 3. 学术元数据 API

| 来源 | 适用能力 | 限制 | 建议 |
| --- | --- | --- | --- |
| [Crossref REST API](https://www.crossref.org/documentation/retrieve-metadata/rest-api/) | `/journals/{issn}/works` 按期刊取 DOI 元数据；`from-index-date` 可找任意新建或变更记录，并支持 cursor（[官方过滤器](https://www.crossref.org/documentation/retrieve-metadata/rest-api/rest-api-filters/)） | 取决于出版社 deposit；摘要、作者或在线日期可能缺失；不是 alert 推送 | 第一补漏源，使用 ISSN + 重叠时间窗 + cursor；带 `mailto` 进入 polite pool |
| [OpenAlex](https://developers.openalex.org/api-reference/introduction) | 统一 works/sources/topics；按期刊 source/ISSN、日期、主题过滤，cursor 深分页；免费 API key 当前为必需（[works API](https://developers.openalex.org/api-reference/works/list-works)） | 不是出版社即时源；字段有二次聚合延迟，配额/计费策略可能变化 | 用于广域发现和 Crossref 缺失字段补充，不作为分钟级 alert |
| [Europe PMC REST API](https://europepmc.org/RestfulWebService) | 生命科学文章/预印本；JSON/XML；可按首次收录或首次发表日期查新记录，`core` 可给摘要与 OA 链接 | 领域限定，不能覆盖全部期刊 | 仅为生命科学 profile 开启；API 页面声明 Apache-2.0 |
| [Semantic Scholar Academic Graph API](https://api.semanticscholar.org/api-docs/graph) | bulk paper search、publicationDate 排序、续传 token、引用图 | 搜索和速率限制不适合精确复刻期刊 TOC；venue 规范化可能不够稳定 | 可用于主题发现/引用扩展，不作为期刊更新主源 |

不要使用 Google Scholar 抓取器作为后端来源：没有官方 API，反自动化和稳定性问题会把现在的 RSS 验证问题换一种形式重演。

## 对当前 Go 后端 + 手机客户端的最小架构

```text
官方 RSS/Atom ───────────────┐
邮件 alert → 私有 Atom ──────┼→ 现有解析/规范化 → DOI 优先去重 → SQLite → 分类 → 认证 API → 手机
Crossref/OpenAlex/EPMC 补漏 ─┘
```

1. **现在不改采集模型。** 保留 RSS 主链路；统计每个 feed 的成功率、连续失败天数、挑战类型和最后成功时间。
2. **对实际失败源做无代码邮件试验。** 每个出版社/期刊使用独立地址和私有 Atom；试用 4–8 周，对比同一期刊 RSS/官网 TOC 的漏报、重复、延迟和字段质量。
3. **只增加一个补漏作业。** 优先 Crossref：按 ISSN 查询，使用 `from-index-date`、小重叠窗口和 cursor；进入现有论文 upsert/metadata enrichment，不另建一套内容模型。
4. **去重规则保持保守。** DOI 规范化后为首选键；无 DOI 时才用规范化标题 + 期刊 + 日期，并把近似匹配留给人工确认，避免误合并。
5. **手机不持有采集凭据。** 邮箱、API key、私有 feed token、挑战会话都留后端。邮件 HTML 入库前清洗脚本、表单、远程图片/追踪像素；外部 URL 获取继续做 SSRF 防护。
6. **先解决远程访问。** 在发布手机端之前，明确后端是云端单用户服务、家庭服务器，还是桌面在线时同步；补上 HTTPS、认证、会话撤销和数据备份。移动 UI 可以复用现有响应式 Web/PWA 作为第一版，不必先写原生 App。

## 建议的决策门槛

- 若受挑战 feed 少于约 10%，逐源改为邮件/元数据补漏，继续以 RSS 为主。
- 若同一出版社大量期刊稳定失败，先评估该出版社 update alert 或 Crossref ISSN 拉取，不先写浏览器自动化。
- 只有当 4–8 周试验确认邮件在覆盖率和延迟上明显优于 RSS，才把 IMAP/webhook 做成一等数据源。
- 只有当移动端确实需要离线、推送或系统分享深度集成时，再从响应式 Web/PWA 升级原生客户端。

## 本轮明确跳过

- 未建议自动破解 CAPTCHA、复用用户 Cookie 或批量无头浏览器绕过；这些方案不稳定，也可能触及站点条款。
- 未建议立刻引入 Miniflux/FreshRSS/RSSHub 作为产品依赖；当前 Go 后端已经拥有解析、存储、分类和阅读状态，重复引入整套阅读器收益小。
- 未设计通用 source interface、邮件服务器或移动离线同步协议；先用私有 Atom 和一个 Crossref 补漏作业验证需求，数据证明不足时不加代码。
