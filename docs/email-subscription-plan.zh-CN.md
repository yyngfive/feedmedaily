# FMD 邮件期刊订阅计划

> 状态：方案草稿
>
> 更新时间：2026-09-11
>
> 目标期刊：Chemical Science、ChemComm
>
> 适用版本：FeedMeDaily release 版

## 1. 目标与边界

RSC 旧 RSS 地址已经出现长期不更新的情况，因此把 RSC 的邮件提醒作为这两个期刊的补充采集源。邮件由第三方邮箱接收，FMD 通过 IMAP over TLS 定时读取；FMD 不运行公网 SMTP 服务，也不承担邮件投递。

```text
RSC 邮件提醒
    ↓
专用第三方邮箱 / 专用文件夹
    ↓ IMAPS 993
FMD 邮件适配器
    ↓
标题、作者、DOI、文章链接、期刊、日期
    ↓
现有入库、去重、分类、报告流程
```

这个方案不要求把 DNS 托管从阿里云迁走。若使用自有域名，只需在阿里云 DNS 中增加邮件服务商要求的记录；若使用 `@gmail.com`、`@outlook.com` 或 `@fastmail.com` 地址，则完全不需要改域名 DNS。

## 2. 第三方邮箱推荐

### 推荐顺序

| 选项 | 适合场景 | IMAP / 安全登录 | 结论 |
| --- | --- | --- | --- |
| 阿里邮箱企业邮 | FMD 在中国大陆本地运行，重视网络可达性，并愿意使用自有域名 | `imap.qiye.aliyun.com:993`，SSL；可使用“三方客户端安全密码” | 当前场景的本地化首选 |
| Fastmail Standard 或更高套餐 | 希望使用专业、独立、长期稳定的专用收件箱 | `imap.fastmail.com:993`，SSL/TLS；使用应用专用密码 | 技术上最干净的长期方案 |
| Gmail | 已经有 Gmail，且运行 FMD 的机器能稳定访问 Google | 支持 IMAP；没有 OAuth 登录时可用应用专用密码，但需要开启两步验证 | 零成本试验首选 |
| Outlook.com | 已经使用微软账号，或组织环境更适合 Microsoft | `outlook.office365.com:993`，SSL/TLS；要求 OAuth2，且 IMAP 可能默认关闭 | 可用，但首版接入复杂度更高 |
| Proton Mail | 对隐私有特殊要求 | 需要付费 Proton Mail Bridge，在本机提供本地 IMAP | 不建议作为 FMD 第一接入目标 |

### 我的具体建议

1. **如果 FMD 主要在中国大陆电脑上运行：优先阿里邮箱企业邮。** 官方提供标准 IMAP SSL 端口 993，并支持三方客户端安全密码。自有域名的 DNS 仍然可以放在阿里云。
2. **如果可以接受付费且网络访问稳定：优先 Fastmail Standard 或更高套餐。** Fastmail 的 Basic 套餐不包含 IMAP 和应用密码；自定义域名的 DNS 可以继续由其他 DNS 服务商托管。
3. **如果只是验证方案：用 Gmail 建一个专用账号。** 不要使用自己的主邮箱；为 FMD 单独生成应用专用密码，并先从运行 FMD 的机器测试 IMAP 连通性。

不建议把 Outlook 作为第一实现目标，因为官方要求 OAuth2/Modern Auth；也不建议使用 Proton Mail，因为 FMD 还要依赖本机持续运行 Bridge。

### 免费路线

如果预算为零，优先按下面顺序试用：

1. **Gmail 专用账号**：免费、直接支持 IMAP；FMD 第一版可使用两步验证生成的应用专用密码。前提是运行 FMD 的机器可以稳定访问 Google。
2. **Outlook.com 专用账号**：免费、支持 IMAP，但当前要求 OAuth2/Modern Auth，并且需要在设置中开启 IMAP；适合后续 FMD 支持 OAuth 后使用。
3. **Yahoo Mail 专用账号**：官方提供 IMAP SSL 993 和应用密码；是否适合长期使用仍应先从 FMD 主机测试网络和账号可用性。
4. **QQ 邮箱或 163 邮箱**：可以作为国内网络环境下的免费候选，但需要在实际账号中确认 IMAP 开关、授权码和服务器参数；在没有完成连接测试前，不把它们作为 FMD 的固定生产配置。

**免费方案的首选是 Gmail；如果 Google 在运行 FMD 的网络中不可稳定访问，就先用 QQ/163 做连通性试验，再决定是否增加国产邮箱适配。** Zoho Mail 免费计划目前主要是 Web-only，新注册免费账号可能没有 IMAP，不建议为这个用途专门注册。

官方依据：

- [阿里邮箱 IMAP、POP、SMTP 服务器和端口](https://help.aliyun.com/zh/document_detail/36576.html)
- [Fastmail 服务器名称和端口](https://www.fastmail.help/hc/en-us/articles/1500000278342-Server-names-and-ports)
- [Fastmail 应用密码](https://www.fastmail.help/hc/en-us/articles/360058752854-App-passwords)
- [Fastmail 自定义域名](https://www.fastmail.help/hc/en-us/articles/360058753394-Custom-domains-with-Fastmail)
- [Gmail IMAP 设置](https://support.google.com/mail/answer/78892?hl=en)
- [Gmail 应用专用密码](https://support.google.com/mail/answer/185833?hl=en-eu)
- [Outlook.com IMAP 设置](https://support.microsoft.com/en-US/Outlook/pop-imap-and-smtp-settings-for-outlook-com)
- [Yahoo Mail IMAP 设置](https://help.yahoo.com/kb/SLN4075.html)
- [Zoho Mail 订阅计划和免费计划限制](https://www.zoho.com/mail/help/adminconsole/subscription.html)
- [Proton Mail Bridge](https://proton.me/support/imap-smtp-and-pop3-setup)

## 3. 邮箱和 DNS 规划

### 3.1 邮箱命名

建立专用地址，不要将个人邮箱直接交给 FMD。推荐名称：

```text
fmd-alerts@你的域名
fmd-rsc@你的域名
```

如果使用公共邮箱，则使用类似以下的专用账号：

```text
fmd.rsc.alerts@fastmail.com
fmd.rsc.alerts@gmail.com
```

### 3.2 自有域名方案

DNS 继续托管在阿里云。配置时遵循以下原则：

- 先确认该域名当前是否已经有生产邮箱；有的话不要直接覆盖根域名 MX。
- 优先考虑专用子域名，例如 `mail.你的域名` 或 `alerts.你的域名`，但要先确认邮箱服务商支持子域名托管。
- 按邮箱服务商要求在阿里云增加 MX、SPF、DKIM、DMARC 或验证 TXT 记录。
- 不修改网站已有的 A、AAAA、CNAME 记录。
- 不把 DNS 托管迁移到邮箱服务商。

DNS 记录的具体值必须以所选服务商当前控制台显示的值为准，不把记录值写死在 FMD 配置或本计划中。

### 3.3 邮箱文件夹

建议创建以下文件夹或标签：

```text
FMD/
├── RSC/
│   ├── Chemical Science/
│   ├── ChemComm/
│   ├── Setup/
│   └── Failed/
└── Other/
```

第一阶段可以只使用 `FMD/RSC` 一个文件夹，收到第一封真实提醒后再根据实际发件人和主题建立规则。不要凭猜测预先写死 RSC 发件地址。

## 4. RSC 邮件订阅步骤

1. 用专用邮箱注册或登录 RSC Publishing 账户。
2. 打开 [RSC Email Alerts](https://www.rsc.org/publishing/journals/email-alerts)。
3. 完成确认邮件中的链接，然后进入 alerts/preferences 页面。
4. 订阅 `Chemical Science` 和 `ChemComm`。
5. 如果平台同时提供“最新文章”和“期刊目录/Issue/TOC”两类提醒，优先开启最新文章；Issue/TOC 可作为补漏。
6. 暂时不要勾选 RSC 新闻、营销邮件或书籍提醒，减少 FMD 解析噪声。
7. 收到第一封提醒后，记录真实的 `From`、`Reply-To`、主题格式和文章链接结构，再配置邮箱规则。
8. 在邮箱中将 RSC 邮件移动到 `FMD/RSC`，但保留原邮件，不自动删除。

RSC 官方说明：期刊内容提醒通过 Publishing 平台管理，订阅后需要确认邮件并保存偏好设置；RSC 也保留邮件底部退订入口。[RSC 邮件提醒说明](https://www.rsc.org/publishing/journals/email-alerts) · [RSC 帮助中心](https://www.rsc.org/help-and-legal/help)

## 5. 邮箱过滤规则

首次收到真实邮件前，不要把发件人地址写死。确认后建立以下规则：

| 条件 | 动作 |
| --- | --- |
| 发件人属于 RSC，且主题包含 `Chemical Science` | 移动到 `FMD/RSC/Chemical Science`，保留副本 |
| 发件人属于 RSC，且主题包含 `ChemComm` 或 `Chemical Communications` | 移动到 `FMD/RSC/ChemComm`，保留副本 |
| RSC 订阅确认邮件 | 移动到 `FMD/RSC/Setup`，保留至少到确认完成 |
| 解析失败或格式不明确的 RSC 邮件 | 不删除，移动到 `FMD/RSC/Failed` |
| 其他 RSC 新闻或营销邮件 | 不进入 FMD，或移动到 `Other` |

FMD 不应只读取“未读邮件”。IMAP 读取进度应使用 UID checkpoint 和 Message-ID；这样即使用户在网页端打开邮件，也不会导致 FMD 漏采集。

## 6. FMD 接入计划

### 当前状态

当前 release 没有邮箱账户配置、IMAP 拉取器或 RSC 邮件解析器。因此这份计划可以先指导邮箱订阅和数据试验，但要让 release 直接收取邮件，需要后续发布包含邮件源适配器的新版本。

### 最小功能集

#### 邮箱连接

- IMAPS over TLS，默认端口 993。
- 账号使用完整邮箱地址。
- 密码使用应用专用密码、三方客户端安全密码或 OAuth 凭据。
- 提供“测试连接”按钮。
- 允许选择 IMAP 文件夹，默认 `FMD/RSC`。
- 默认每 15 分钟轮询一次，允许手动立即同步。

#### 增量读取

- 按邮箱账号、文件夹保存 UID checkpoint。
- 同时保存 Message-ID，防止服务端 UID 变化时重复入库。
- 只有文章成功写入数据库后才更新 checkpoint。
- 默认不标记已读、不删除邮件；后续可以增加“成功后标记已读”的选项。
- FMD 重启后继续处理邮箱中尚未确认成功的邮件。

#### 邮件解析

每封邮件转换成一个或多个 article candidate。需要解析：

- 期刊名称；
- 文章标题；
- 作者；
- DOI；
- RSC 文章链接；
- 文章发布日期或在线发布日期；
- 邮件接收时间；
- 原始邮件 Message-ID 和解析器版本。

优先从文章内容提取日期，不要把邮件头部的 `Date` 直接当成发表日期。邮件头部时间只作为缺失时的回退值。

#### 去重和复用现有流程

- DOI 规范化后作为第一去重键。
- 没有 DOI 时使用规范化链接。
- 仍没有稳定标识时，才使用“期刊 + 标题 + 日期”的保守组合。
- 解析后的 article candidate 进入现有论文 upsert、分类和报告流程，不另外建立一套邮件专用论文模型。
- 邮件 HTML 入库前去除脚本、表单、追踪像素和不必要的远程资源。

### 概念配置

以下只是设计示例，不是当前 release 可直接粘贴的配置；实际密码不能写入文档或日志。

```yaml
mailSources:
  - id: rsc-alerts
    type: imap
    host: imap.qiye.aliyun.com
    port: 993
    tls: true
    username: fmd-alerts@your-domain.example
    credentialRef: fmd-rsc-imap
    mailbox: FMD/RSC
    pollInterval: 15m
    markRead: false
    deleteAfterImport: false
    allowedSenders: []
```

`host` 应根据实际服务商替换。例如 Fastmail 使用 `imap.fastmail.com`，Gmail 使用其官方 IMAP 主机；不要因为 DNS 在阿里云就把所有邮箱都配置成阿里邮箱服务器。

## 7. 安全和运行数据

- 不在 Git、Markdown、截图、日志或普通 UI 配置导出中保存真实密码。
- release 模式的邮箱凭据应进入本机安全存储或受保护的运行时配置；source 模式只使用本地 `.env` 等被 Git 忽略的文件。
- 应用专用密码只授予邮件访问权限，并且只创建一个给 FMD 使用的凭据。
- 凭据泄露时只撤销该应用密码，不需要更换整个邮箱账户密码。
- 日志中只显示邮箱服务商、文件夹、UID 和错误类型，不显示邮件正文、授权头或密码。
- 备份 FMD 数据库时，将邮箱凭据与数据库备份分开保护。

## 8. 分阶段实施

### 阶段 A：邮箱验证

- 建立专用邮箱或专用域名地址。
- 开启两步验证，并创建应用密码或三方客户端安全密码。
- 从运行 FMD 的 Windows 主机测试 `993/TLS` 连通性。
- 在邮箱网页端确认能创建文件夹和过滤规则。

### 阶段 B：RSC 订阅试验

- 只订阅 Chemical Science 和 ChemComm。
- 保存订阅确认邮件和第一批提醒邮件。
- 对比邮箱提醒与 RSC 期刊页面，确认是否为单篇文章、Issue 目录或混合摘要。
- 记录发件人、主题、HTML 文章块、DOI 和链接字段。

### 阶段 C：FMD 邮件源

- 增加通用 IMAP source adapter。
- 增加 RSC 邮件模板解析器。
- 接入现有 upsert、DOI 去重、分类和报告任务。
- 增加同步状态、解析失败数和最后成功时间。
- 加入重启恢复、重复邮件和多文章 digest 测试。

### 阶段 D：观察期

至少观察 4–8 周，记录：

- 邮件到达延迟；
- 文章覆盖率；
- 重复率；
- 解析失败率；
- 邮箱被判垃圾邮件的情况；
- 邮件模板变化；
- 与 Crossref 或 RSC 期刊页面的差异。

邮件通道只有在观察期证明稳定后，才应成为 release 中的正式数据源。RSS 可以继续保留，邮件负责补充或替代已经确认停更的 RSC feed。

## 9. 验收标准

- 一封包含多篇文章的 RSC 邮件能拆成多篇论文。
- 相同文章重复同步不会产生重复论文。
- FMD 关闭后重新启动可以继续从上次 UID 读取。
- 用户在网页端把邮件标为已读，不会导致 FMD 漏采集。
- 文章日期优先使用文章内容中的发布日期。
- 解析失败邮件仍保留在邮箱中并可定位原因。
- 邮箱密码、OAuth token 和邮件正文不会出现在日志中。
- RSC 主题或 HTML 轻微变化时有明确失败日志，而不是静默丢数据。

## 10. 最终决策

本项目建议按**通用 IMAP 接入 + RSC 专用解析器**设计，不把 FMD 绑定到某一家邮箱。

第一选择：

- 中国大陆本地运行、已有阿里云 DNS：阿里邮箱企业邮；
- 可接受付费且国际网络稳定：Fastmail Standard 或更高套餐；
- 只做快速验证：Gmail 专用账号。

无论选择哪一家，邮箱都只负责稳定保存和提供 IMAP 访问，文章入库、去重、分类和报告仍由 FMD 负责。

## 11. 多用户部署：保留阿里云 DNS、避免 ECS

### 11.1 推荐架构：Forward Email + Cloudflare Workers + AliDNS

如果 FMD 是多用户服务，不应让每个用户登录同一个邮箱。建议改成集中收信、分配独立 feed：

```text
RSC
  ↓ 发到每个 feed 的独立地址
Forward Email
  ↓ HTTPS webhook
Cloudflare Worker + KV
  ↓
用户自己的 RSS/Atom feed
  ↓
FMD 用户只保存 feed URL，不保存邮箱凭据
```

具体做法：

1. 在阿里云 DNS 中为邮件入口准备一个专用子域名，例如 `inbox.example.com`。
2. 将该子域名的 MX 记录指向 Forward Email 的 MX 服务器。
3. 按 Forward Email 的 webhook 规则，在阿里云 DNS 添加 TXT 记录，把邮件投递到 Worker 的 HTTPS webhook。
4. 将 `kill-the-news` 部署到自己的 Cloudflare 账号，使用 `workers.dev` 地址作为 webhook 和 feed 的访问地址，或者对项目做少量配置改造。
5. 每个 FMD 用户或每个期刊生成独立的随机收件地址和独立的 Atom URL。

Forward Email 官方支持免费计划通过 DNS TXT 配置 webhook，并会提供解析后的邮件 JSON、原始邮件、收件人、发件人和附件信息；webhook 失败时会重试。[Forward Email webhook 文档](https://forwardemail.net/en/faq)

`kill-the-news` 本身已经支持 ForwardEmail webhook、独立的入站地址与 feed URL、发送者白名单、Atom/RSS/JSON Feed 和 Cloudflare KV 存储。[kill-the-news 功能和架构](https://github.com/juherr/kill-the-news)

Cloudflare Workers Free 计划目前包含有限的 Worker、KV 和 D1 配额，例如 KV 每日读取、写入和存储量都有上限；对于少量期刊和用户通常足够，但不能把“免费”当成无限容量保证。[Cloudflare Workers 价格与配额](https://developers.cloudflare.com/workers/platform/pricing/) · [Workers 限制](https://developers.cloudflare.com/workers/platform/limits/)

### 11.2 重要限制

`kill-the-news` 官方的一键安装路径要求域名作为 Cloudflare zone，并由 Cloudflare 管理 DNS；其直接 Email Workers 接收方式会要求 Cloudflare Email Routing。因此，**它不能在保持整个域名完全由阿里云 DNS 管理的前提下原样一键部署**。[官方安装要求](https://github.com/juherr/kill-the-news/blob/main/INSTALL.md)

上面的 Forward Email webhook 路径绕开了 Cloudflare Email Routing，所以可以继续使用阿里云 DNS；但 feed URL 默认会使用 `workers.dev` 域名。若必须使用 `feed.example.com`，就需要额外配置 Cloudflare 自定义域名，或把 feed 服务改放到阿里云函数计算等支持自定义域名的平台。

免费 Forward Email 计划的 webhook 配置方式是公开 TXT 规则，且其文档提示免费计划的转发地址可能可被公开搜索。生产环境应把 feed URL 当作密码，使用随机不可猜测的 feed token，并开启发送者白名单；如果需要更强的地址隐私，应评估付费计划。[Forward Email 免费计划说明](https://forwardemail.net/en/faq)

实际验证时，`stassenger.top` 被 Forward Email 控制台判定为“经常用于垃圾邮件操作的域名扩展名”，免费计划直接要求升级。因此，**Forward Email 免费方案并不适用于当前这个 `.top` 域名**；这不是阿里云 DNS、MX、TXT 或 Worker 配置错误。保留当前域名的最短路径是升级 Forward Email（页面显示年付 36 美元），或者换用一个未触发该限制的域名；也可以暂时改用不依赖自有域名的 Kill the Newsletter!。升级或换域名后，仍可保留 AliDNS 和现有 Worker 架构。

### 11.3 阿里云原生方案：函数计算 FC

也可以使用以下架构，不租 ECS：

```text
RSC → Forward Email → 阿里云函数计算 HTTP 触发器
                         ↓
                 OSS / 表格存储保存邮件和 feed
                         ↓
                    Atom/RSS HTTP 接口
```

函数计算支持 HTTP 触发器、自定义域名和 API 网关；自有域名仍可在阿里云 DNS 中用 CNAME 配置。[函数计算 HTTP 触发器](https://help.aliyun.com/zh/functioncompute/http-triggers-overview) · [函数计算自定义域名](https://help.aliyun.com/zh/functioncompute/configure-custom-domain-names)

但 `kill-the-news` 是 Cloudflare Workers 项目，不能直接上传到函数计算。需要重新实现邮件 webhook、持久化、管理后台和 Atom/RSS 输出，或者维护一个适配层。因此它适合后续完全阿里云化，不适合作为第一版验证路线。函数计算也应按量计费和试用额度设计，不能假设永久零成本。

### 11.4 最快的无服务器验证

如果暂时不需要自有域名和自有数据存储，直接使用 [Kill the Newsletter!](https://kill-the-newsletter.com/) 最省事：它为每个 feed 生成一个独立邮箱地址和 Atom feed，FMD 用户只订阅自己的 Atom URL，不需要登录共享邮箱，也不需要 DNS 或服务器。

它适合验证 RSC 邮件模板是否可用，但不适合作为长期多租户核心依赖：数据在第三方服务上，旧条目可能因 feed 大小限制被清理，feed URL 也必须按密码保护。

### 11.5 多用户数据模型

如果所有 FMD 用户都需要同样的两个期刊，建议采用“期刊级 feed”：

```text
Chemical Science → 一个 RSC 订阅地址 → 一个私有 Atom feed → 多个 FMD 用户读取
ChemComm         → 一个 RSC 订阅地址 → 一个私有 Atom feed → 多个 FMD 用户读取
```

如果用户需要不同期刊组合，则使用“用户级 feed”：

```text
user-A / Chemical Science → 独立收件地址 → 独立 feed token
user-B / ChemComm         → 独立收件地址 → 独立 feed token
```

FMD 只保存 feed URL 和来源标识，不保存 RSC 邮箱凭据。Feed URL 使用随机 token，不能把收件地址直接当作 feed URL。

### 11.6 最终建议

- **最快验证**：先用公共 Kill the Newsletter!，确认 RSC 邮件格式和 FMD Atom 解析效果。
- **正式的无 ECS 方案**：Forward Email + Cloudflare Worker/KV，阿里云 DNS 只负责 MX/TXT，Worker 使用 `workers.dev` feed 地址。
- **完全阿里云方案**：Forward Email + 函数计算 + OSS/表格存储，但需要自行开发或移植一套 KTN 类服务。
- **不要直接采用 KTN 的 Cloudflare Email Routing 一键部署**，因为它要求域名 DNS 迁移或至少转由 Cloudflare 管理。

## 12. 方案 2 实操手册：AliDNS + Forward Email + `workers.dev`

下面的步骤不改变根域名的 DNS 托管，也不需要 ECS。示例中：

```text
主域名：example.com
邮件入口：inbox.example.com
Worker 名称：fmd-kill-the-news
Cloudflare 账号子域名：my-account
Worker 地址：https://fmd-kill-the-news.my-account.workers.dev
```

把示例域名和账号子域名替换成自己的值。`workers.dev` 账号子域名需要先在 Cloudflare 的 Workers & Pages 页面启用；Worker URL 的格式是“Worker 名称 + 账号子域名 + workers.dev”。[Cloudflare workers.dev 文档](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/)

### 12.1 准备账号和本地环境

需要：

- Cloudflare 免费账号，并启用一个 `workers.dev` 账号子域名；
- Forward Email 账号；
- Node.js 20 或更高版本；
- Windows 上建议使用 Git Bash 或 WSL 执行仓库的 Bash 脚本；
- 一个没有承载现有业务邮箱的专用子域名，例如 `inbox.example.com`。

不要把现有邮箱所在的根域名 `@` MX 记录改到 Forward Email；本方案只操作 `inbox` 这个子域名。

### 12.2 部署 `kill-the-news`，但不使用其自定义域名路由

官方安装脚本默认要求域名接入 Cloudflare，并会生成 `custom_domain` 路由。保留 AliDNS 时，不要直接照搬这部分；可以先克隆仓库，再手工生成配置：

```bash
git clone https://github.com/juherr/kill-the-news.git fmd-kill-the-news
cd fmd-kill-the-news
npm install
npx wrangler login
npx wrangler kv namespace create EMAIL_STORAGE
npx wrangler kv namespace create EMAIL_STORAGE --preview
cp wrangler-example.toml wrangler.toml
```

把两条 `kv namespace create` 输出的 ID 填入 `wrangler.toml`。核心配置可以按下面调整；不要把真实密码写入文件：

```toml
name = "fmd-kill-the-news"
main = "src/index.ts"
compatibility_date = "2026-09-11"
compatibility_flags = ["nodejs_compat"]

kv_namespaces = [
  { binding = "EMAIL_STORAGE", id = "生产 KV ID", preview_id = "预览 KV ID" }
]

[vars]
# feed 和 webhook 的 Web 地址，不是收件域名
DOMAIN = "fmd-kill-the-news.my-account.workers.dev"
# 邮件收件地址所在的域名，可以和 DOMAIN 不同
EMAIL_DOMAIN = "inbox.example.com"
FEED_MAX_SIZE_BYTES = "1048576"
ATTACHMENTS_ENABLED = "false"

[env.production]
workers_dev = true
kv_namespaces = [
  { binding = "EMAIL_STORAGE", id = "生产 KV ID" }
]

[env.production.vars]
DOMAIN = "fmd-kill-the-news.my-account.workers.dev"
EMAIL_DOMAIN = "inbox.example.com"
FEED_MAX_SIZE_BYTES = "1048576"
ATTACHMENTS_ENABLED = "false"
```

重点是：

- `workers_dev = true`；
- 删除或不要添加官方模板中的 `routes = [...]` 和 `custom_domain = true`；
- `DOMAIN` 填 Worker 的 `workers.dev` 主机名，不要带 `https://`；
- `EMAIL_DOMAIN` 填收件子域名；
- `ADMIN_PASSWORD` 用 Worker secret 保存。

这里有一个需要明确标注的兼容性边界：当前 `kill-the-news` 官方生产模板把 `workers_dev` 设为 `false`，并用 Cloudflare `custom_domain` 路由；官方安装文档也要求域名作为 Cloudflare zone。下面把它改成 `workers.dev` 是手工部署/验证路径，不是项目文档明确保证的生产组合。因此先用个人测试邮件验证完整链路，再把 RSC 期刊提醒切过去；如果 Worker 在 `workers.dev` 上出现路由或生产限制，就需要把域名接入 Cloudflare，或改用阿里云函数计算重写接收和 feed 层。

设置管理员密码并部署：

```bash
npx wrangler secret put ADMIN_PASSWORD --env production --name fmd-kill-the-news
npm run deploy
```

官方安装文档也使用 Node.js 20+、KV namespace、`ADMIN_PASSWORD` secret 和 `npm run deploy` 这条流程，但其默认前提是域名 DNS 由 Cloudflare 管理；这里仅保留它的 Worker/KV 部分。[kill-the-news 安装文档](https://github.com/juherr/kill-the-news/blob/main/INSTALL.md) · [官方 Wrangler 模板](https://raw.githubusercontent.com/juherr/kill-the-news/main/wrangler-example.toml)

部署成功后先打开：

```text
https://fmd-kill-the-news.my-account.workers.dev/admin
```

### 12.3 在阿里云 DNS 添加 Forward Email 记录

在阿里云 DNS 控制台添加以下记录。主机记录都是 `inbox`，不是 `@`：

| 主机记录 | 类型 | 记录值 | 优先级 |
|---|---|---|---:|
| `inbox` | MX | `mx1.forwardemail.net` | 10 |
| `inbox` | MX | `mx2.forwardemail.net` | 10 |
| `inbox` | TXT | `forward-email=https://fmd-kill-the-news.my-account.workers.dev/api/inbound` | — |

这条不带别名的 webhook 规则用于把 `*@inbox.example.com` 的邮件都发到 Worker；Worker 会根据邮件的收件人匹配对应 feed。Forward Email 的免费计划支持通过 DNS TXT 配置 webhook，且 webhook 地址必须带 `https://`。[Forward Email FAQ](https://forwardemail.net/en/faq)

如果 Forward Email 控制台额外给出 `forward-email-site-verification=` 或其他验证 TXT，请按控制台原样添加，不要自行修改。不要在同一个主机重复添加多个互相冲突的 SPF TXT；本方案只接收邮件，第一次测试可以先不加 SPF。

等待 DNS 生效后，在 PowerShell 中检查：

```powershell
Resolve-DnsName inbox.example.com -Type MX
Resolve-DnsName inbox.example.com -Type TXT
```

应该能看到两个 Forward Email MX 和包含 `/api/inbound` 的 TXT。若根域名已有正常邮箱，检查时也要确认没有误改 `example.com` 的 `@` 记录。

### 12.4 在 `kill-the-news` 中生成期刊收件地址

1. 打开 `https://fmd-kill-the-news.my-account.workers.dev/admin` 并登录。
2. 新建一个 feed，命名为 `Chemical Science`。
3. 再新建一个 feed，命名为 `ChemComm`。
4. 分别复制每个 feed 的 **Inbound email address**，例如 `random.name.42@inbox.example.com`。
5. 到 RSC 的邮件提醒页面，用对应地址订阅对应期刊。
6. 如果 RSC 发来确认邮件，回到 KTN 管理后台打开该邮件，点击 **Confirm your subscription** 里的确认链接；KTN 默认只检测并展示链接，不会自动替你点击。[kill-the-news 的确认邮件说明](https://github.com/juherr/kill-the-news/blob/main/INSTALL.md)

不要把 Inbound address 当作给 FMD 使用的 URL。管理后台还会给出独立的 Atom URL，例如：

```text
https://fmd-kill-the-news.my-account.workers.dev/atom/<opaque-feed-id>
```

把这个 Atom URL 添加到 FMD；多个 FMD 用户可以读取同一个期刊级 Atom URL，而不需要登录同一个邮箱。

### 12.5 逐层测试

建议先不要直接等 RSC，先从个人邮箱发一封测试邮件到 KTN 生成的 Inbound address：

1. 邮件能否到达 `inbox.example.com` 的 MX；
2. Worker 管理后台是否出现新邮件；
3. Atom URL 是否返回新条目；
4. FMD 刷新后是否能看到该条目；
5. 最后再把 RSC 的订阅地址改成同一个 Inbound address。

常见故障定位：

- MX 查不到：阿里云记录的主机名或 DNS 生效有问题；
- MX 正常但没有邮件：检查 TXT 的主机名是否是 `inbox`、webhook 是否精确为 `/api/inbound`；
- Worker 能收到测试邮件但 feed 没内容：检查 `EMAIL_DOMAIN`、收件地址是否复制完整、是否用了对应的 Atom URL；
- 只有旧邮件：默认 feed 会按存储大小清理最旧条目；本示例把阈值调到 1 MiB，仍然不是永久存档；
- 期刊确认邮件没有触发订阅：在 KTN 管理后台手动点击确认链接，再等待下一封期刊提醒。

### 12.6 免费方案的边界

Cloudflare Workers Free 当前是每天 100,000 次 Worker 请求、每次 10 ms CPU；Workers KV Free 是每天 100,000 次读取、1,000 次写入、1,000 次删除、1,000 次 list、1 GB 存储。对少量期刊和用户一般够用，但不是无限免费容量；超过限制后相应操作会失败。[Cloudflare Workers 价格](https://developers.cloudflare.com/workers/platform/pricing/) · [Cloudflare KV 价格](https://developers.cloudflare.com/kv/platform/pricing/) · [Workers 限制](https://developers.cloudflare.com/workers/platform/limits/)

Forward Email 免费计划通常可通过 DNS 配置 webhook，但具体域名仍可能触发其反滥用限制。当前 `stassenger.top` 已被控制台要求升级，因此本域名必须使用付费计划或换用其他域名。对于可用的免费域名，仍建议使用随机收件地址、不要在公开页面发布 Atom URL，并在 KTN 中设置允许的发件人域名；Atom URL 本质上是 bearer token，知道 URL 的人就能读取 feed。

### 12.8 Forward Email 虚荣域名 `hideaddress.net` 备用路径

如果 Forward Email 账户允许使用 `hideaddress.net` 虚荣域名，可以绕开 `stassenger.top` 的 `.top` 限制，同时继续保留 AliDNS。此域名不需要在 AliDNS 添加 MX/TXT；邮件由 Forward Email 自己接收。但截图提示虚荣域名不支持 catch-all，官方 FAQ 也说明全局虚荣域名不支持正则别名，因此必须为每个 KTN feed 单独创建 alias。

操作顺序：

1. 将 KTN 的全局和 production `EMAIL_DOMAIN` 都改为 `hideaddress.net`，`DOMAIN` 仍保持 Worker 的 `workers.dev` 地址。
2. 重新执行 `npm run deploy`。
3. 打开 KTN 管理后台，为 `Chemical Science` 和 `ChemComm` 创建或查看 feed，并复制各自完整的 Inbound address。地址应类似 `noun.noun.42@hideaddress.net`。
4. 回到 Forward Email 的“添加别名”页面：域名选 `hideaddress.net`，姓名只填写地址的本地部分，例如 `noun.noun.42`，勾选“积极的”。
5. “转发收件人”填写 Worker webhook，而不是普通邮箱：

   ```text
   https://fmd-kill-the-news-production.stassenger.workers.dev/api/inbound
   ```

   如果还想保留一份副本，可以另加 `chenhye5@outlook.com`；第一次测试建议只填 webhook，减少变量。
6. 用生成的 `@hideaddress.net` 地址去订阅 RSC，确认邮件会在 KTN 管理后台出现；确认后把 KTN 给出的 Atom URL 添加到 FMD。

截图中的 `chenhye5@outlook.com` 只是普通转发目标；如果只保留它，邮件不会进入 KTN。`hideaddress.net` 也可能被个别期刊当作虚荣/一次性域名拒绝，遇到这种情况只能改用可控且未被拦截的自有域名或使用 Forward Email 付费计划。

### 12.7 这条路线的实际结论

本方案的最小闭环是：

```text
RSC → inbox.example.com → Forward Email → Worker /api/inbound
                                      ↓
                             Cloudflare KV
                                      ↓
                         /atom/<opaque-feed-id> → FMD
```

它保留 AliDNS、不需要 ECS，也避免多个 FMD 用户共享邮箱密码。代价是需要手工维护一个 `workers.dev` Worker 和一组阿里云 MX/TXT 记录，而且这条 `workers.dev` 生产组合没有被 `kill-the-news` 官方安装文档明确保证，必须先做端到端测试；如果以后需要 `feed.example.com` 这样的自定义 feed 域名，再单独评估把该子域名接入 Cloudflare，而不是迁移整个主域名。
