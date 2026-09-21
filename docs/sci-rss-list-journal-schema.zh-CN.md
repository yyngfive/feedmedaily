# Sci-RSS-List 期刊与 Feed 身份字段说明

> 状态：Sci-RSS-List 当前数据已采用本文描述的字段。本文记录字段约定及 FeedMeDaily 的导入规则。

## 目标

将“刊物身份”和“RSS feed 类型”拆成独立字段，使导入程序可以直接采用上游标准刊名，不必从刊名字符串、出版社域名或自由文本备注中推断单刊、聚合源和学科分类源。

为兼容旧客户端，`journal` 保留订阅标签，仍可能包含 feed 类型或目录分区。新客户端使用独立字段识别刊物身份和 feed 范围：例如 ACS 同一期刊的 ASAP 与 Current Issue 共享标准刊名，APS 的分区与跨刊聚合源则各自明确标注。仅靠删除括号、冒号或卷期文字会误伤真实刊名或目录分区。

## 字段定义

保留现有 `journal`、`url`、`publisher`、`subjects`、`source`、`method`、`status` 和 `notes`。新增字段使用 snake_case，并保持 `data/feeds.json` 当前根数组结构不变。

| 字段                  | 规则                                                                                                                                                                                                     |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `canonical_journal` | 单刊 feed 的正式全名；不包含 RSS 类型或文章卷期。聚合源、平台源和学科分类源填`null`。                                                                                                                  |
| `issn_l`            | 可选的 ISSN-L，用于同刊多条 feed 共享稳定身份；尚未核实则填`null`。                                                                                                                                    |
| `feed_scope`        | 必填枚举：`single_journal`、`multi_journal`、`subject_collection`、`platform_collection`。                                                                                                       |
| `feed_type`         | 稳定的小写下划线值，如`asap`、`current_issue`、`recently_published`、`recently_accepted`、`editors_suggestions`、`toc_section`、`subject_collection`、`latest_preprints`。无额外类型的普通单刊 feed 可填`null`。 |
| `feed_name`         | 与 `feed_type` 同时填写或同时为`null`；给用户看的该条 feed 标签，如`ASAP`、`Recently Published` 或 `Atomic, Molecular, and Optical Physics`。 |
| `collection`        | 仅学科分类源使用：包含`platform`、稳定 `id` 和正式分类名 `name`。其他 feed 可省略或填 `null`。                                                                                                   |

兼容约定：`journal` 保留现有值供旧版客户端作为订阅标签使用；新客户端以 `canonical_journal` 判断真实刊名，以 `feed_name` 区分同刊的不同订阅源。不要在未迁移旧客户端的情况下改变 `journal` 的含义。

## 刊名格式

- `canonical_journal` 使用期刊官网公布的正式全名、大小写和标点；同一期刊的所有 feed 必须填写相同值。
- 不把 ASAP、Current Issue、Recently Published、Recently Accepted、Editors' Suggestions、卷期、页码或日期放入 `canonical_journal`。
- 不用通用规则删除括号、冒号或副标题。若文字属于正式刊名或 feed 所覆盖的目录分区，应保留在对应字段。
- `issn_l` 已确认时，同一期刊各 feed 应使用相同值；没有确认时留空，不自行猜测。
- `subjects` 继续保留宽泛检索标签，不用于表达 `feed_scope` 或 subject collection 身份。

## 条目示例

### 同刊的 ACS ASAP 与 Current Issue

两条记录保留各自 URL 和旧版 `journal` 标签，但共享 `canonical_journal` 与 `issn_l`：

```json
{
  "publisher": "ACS",
  "journal": "Journal of the American Chemical Society (ASAP)",
  "canonical_journal": "Journal of the American Chemical Society",
  "issn_l": "<经核实的 ISSN-L>",
  "feed_scope": "single_journal",
  "feed_type": "asap",
  "feed_name": "ASAP",
  "url": "https://pubs.acs.org/rss/jacsat/asap.xml"
}
```

Current Issue 记录采用相同刊名和 ISSN-L，`feed_type` 改为 `current_issue`，`feed_name` 改为 `Current Issue`，URL 使用对应的 current issue 地址。

### APS 单刊 feed 与目录分区

```json
{
  "publisher": "APS",
  "journal": "Physical Review Letters (Recently Published)",
  "canonical_journal": "Physical Review Letters",
  "issn_l": "<经核实的 ISSN-L>",
  "feed_scope": "single_journal",
  "feed_type": "recently_published",
  "feed_name": "Recently Published",
  "url": "https://feeds.aps.org/rss/recent/prl.xml"
}
```

APS 的 PRL 目录分区 feed 仍映射到 `Physical Review Letters`，并把分区名放进 `feed_name`，`feed_type` 设为 `toc_section`。这样不会把冒号后的真实分区误判成刊名的一部分或普通后缀。

### APS 跨刊聚合源

```json
{
  "publisher": "APS",
  "journal": "APS Journals (All Editors' Suggestions)",
  "canonical_journal": null,
  "issn_l": null,
  "feed_scope": "multi_journal",
  "feed_type": "editors_suggestions",
  "feed_name": "All Editors' Suggestions",
  "url": "https://feeds.aps.org/rss/allsuggestions.xml"
}
```

这类 feed 不应把其所有文章归到一个期刊；客户端应优先读取每篇文章自己的刊名。

### bioRxiv/medRxiv 学科分类源

```json
{
  "publisher": "bioRxiv/medRxiv",
  "journal": "medRxiv: Health Economics",
  "canonical_journal": null,
  "issn_l": null,
  "feed_scope": "subject_collection",
  "feed_type": "subject_collection",
  "feed_name": "Health Economics",
  "collection": {
    "platform": "medRxiv",
    "id": "health_economics",
    "name": "Health Economics"
  },
  "url": "https://connect.medrxiv.org/medrxiv_xml.php?subject=health_economics"
}
```

客户端可用 `platform` 和 `collection.name` 构造 `medRxiv: Health Economics` 的分类显示名；该分类不冒充期刊名。

## 客户端解析约定

1. `single_journal`：先按精确 feed URL 读取 `canonical_journal`。即使文章级 `publicationName` 带有卷期或其他引文内容，也使用目录中的单刊名。
2. `multi_journal`：优先使用文章级刊名；仅在文章级刊名缺失时使用 feed 标签。
3. `subject_collection`：以 `collection` 标识分类，并显示平台加分类名。
4. `platform_collection`：使用目录中的平台 feed 身份；只有 `multi_journal` 明确表示跨刊聚合，因此才优先使用文章级刊名。
5. 没有新字段的旧条目只通过有限的兼容逻辑判断；上游新增或修订条目应显式提供 `feed_scope`，不得要求客户端维护出版社 URL 白名单。通用卷期清理仅在没有适用 URL 映射时兜底，并且只有清理后的名称能匹配目录刊名才生效。

## 数据校验条件

- 同一刊物的多个 feed 使用相同 `canonical_journal` 和 `issn_l`，但保留不同 URL、`feed_type` 和 `feed_name`。
- `single_journal` 必须提供非空 `canonical_journal`；其他 scope 的 `canonical_journal` 为 `null`。
- `multi_journal` 条目不得伪装成一个刊名，subject collection 必须提供平台与稳定分类 ID。
- 生成的出版社索引仍保留全部 URL 和现有验证状态、来源与方法信息。
- 旧客户端忽略新字段时仍可读取原有 `journal` 标签。
