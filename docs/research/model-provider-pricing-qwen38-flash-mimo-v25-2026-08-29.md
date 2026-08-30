# Qwen3.8-Flash 与 MiMo-V2.5 官方按量付费价格核验

核验日期：2026-08-29

范围：官方公开的按量付费价格，不包含 Token Plan、PTU、Batch 等非实时按量场景；未调用任何付费 API。价格会变化，代码中的价格快照应带日期和来源。

## 结论摘要

本项目当前场景应采用以下一组价格：

| 模型与场景 | Model ID | OpenAI 兼容 Base URL | 普通输入 | 缓存命中输入 | 输出（含思考 Token） |
| --- | --- | --- | ---: | ---: | ---: |
| Qwen，中国大陆百炼华北 2（北京） | `qwen3.8-flash` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | ¥0.8 / 百万 Token | ¥0.1 / 百万 Token | ¥2.7 / 百万 Token |
| MiMo，中国大陆开放平台普通按量 Key | `mimo-v2.5` | `https://api.xiaomimimo.com/v1` | ¥1.00 / 百万 Token | ¥0.02 / 百万 Token | ¥2.00 / 百万 Token |

依据：[Qwen3.8-Flash 官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)、[百炼 API Key 与 Base URL](https://help.aliyun.com/zh/model-studio/get-api-key/)、[MiMo 按量付费价格](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)、[MiMo-V2.5 官方模型页](https://mimo.mi.com/models/mimo-v2.5)

## 1. Qwen3.8-Flash

### 模型、区域与端点

- API 模型 ID：`qwen3.8-flash`。[官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)
- 本项目的中国大陆百炼共享端点：`https://dashscope.aliyuncs.com/compatible-mode/v1`，对应华北 2（北京）价格。国际版 Key 应改用 `https://dashscope-intl.aliyuncs.com/compatible-mode/v1`，不能套用北京价格。[获取与配置 API Key](https://help.aliyun.com/zh/model-studio/get-api-key/)
- 百炼官方还在美国（弗吉尼亚）、德国（法兰克福）、日本（东京）和新加坡提供该模型；价格页以地域和部署范围分别列价。[官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)

### 官方公开标准价

单位均为人民币元 / 百万 Token。Qwen3.8-Flash 在 0 < 单次请求输入 Token ≤ 1M 范围内不分输入长度阶梯；思考与非思考模式使用相同输入单价。[百炼模型价格](https://help.aliyun.com/zh/model-studio/model-pricing)、[官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)

| 地域 | 部署范围 | 普通输入 | 输入（缓存命中） | 输出 | 显式缓存创建 | 显式缓存命中 |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 华北 2（北京） | 中国大陆 | ¥0.8 | ¥0.1 | ¥2.7 | ¥1.25 | ¥0.1 |
| 美国（弗吉尼亚） | 全球 | ¥0.8 | ¥0.1 | ¥2.7 | ¥1.25 | ¥0.1 |
| 德国（法兰克福） | 全球 | ¥0.8 | ¥0.1 | ¥2.7 | ¥1.25 | ¥0.1 |
| 日本（东京） | 全球 | ¥0.8 | ¥0.1 | ¥2.7 | ¥1.25 | ¥0.1 |
| 新加坡 | 国际 | ¥1.094 | ¥0.117 | ¥3.427 | ¥1.458 | ¥0.117 |

> 这里的“输入（缓存命中）”是模型页列出的缓存命中输入价。显式缓存创建单列，是因为它不是普通未命中输入价：创建缓存按独立价格计费。[Qwen Context Cache](https://help.aliyun.com/zh/model-studio/context-cache)

Alibaba Cloud 国际站另以美元计价。其公开总表给出的实时输入/输出价如下；该表没有展开 qwen3.8-flash 的缓存命中美元单价，因此不应自行套用通用缓存折扣，具体缓存价以国际站控制台账单为准。[Alibaba Cloud 国际站模型价格](https://www.alibabacloud.com/help/en/model-studio/model-pricing)、[Qwen Context Cache](https://help.aliyun.com/en/model-studio/context-cache)

| 国际站服务地域 | 部署范围 | 普通输入（美元 / 百万 Token） | 输出（美元 / 百万 Token） |
| --- | --- | ---: | ---: |
| 新加坡 | International | $0.15 | $0.47 |
| 北京 | 国际站北京地域 | $0.113 | $0.382 |
| 中国香港、德国、美国、日本 | Global | $0.113 | $0.382 |

中国站人民币价格与 Alibaba Cloud 国际站美元价格属于不同账户/服务体系，不能把人民币缓存价直接换算后作为国际站账单规则。

### 思考 Token 如何计费

- 百炼价格表把 Qwen3.8-Flash 的输出栏明确标为“思维链 + 回答”，且同一行注明模型支持“非思考和思考模式”。因此没有独立的思考 Token 单价：开启思考后，思维链 Token 与最终回答 Token 一并按输出价计费。[百炼模型价格](https://help.aliyun.com/zh/model-studio/model-pricing)
- 这也是分类任务显式关闭 thinking 能降低成本的原因：关闭后不会额外产生按输出价计费的思维链 Token。

### 免费额度、促销与有效期

- 阿里云中国站的华北 2（北京）公开表列出 100 万 Token 免费额度；有效期为“自开通百炼、模型发布或申请通过之日起 90 天内，以较晚者为准”。中国站其他地域无这项公开免费额度。[百炼模型价格](https://help.aliyun.com/zh/model-studio/model-pricing)
- Alibaba Cloud 国际站则把同样为期 90 天的 100 万 Token 免费额度列在新加坡 International，其他国际站部署范围无免费额度。两套账户体系的免费额度不能混用。[Alibaba Cloud 国际站模型价格](https://www.alibabacloud.com/help/en/model-studio/model-pricing)
- 官方模型页明确说明页面展示模型调用原价，不包含限时优惠等活动信息，并要求前往百炼控制台查看活动优惠。因此上表适合保存为公开标准价格快照；若账户控制台显示专属或限时折扣，应以控制台实际账单为准，不能在代码里假定该折扣长期有效。[Qwen3.8-Flash 官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)

## 2. MiMo-V2.5

### 模型与端点

- API 模型 ID：`mimo-v2.5`。[MiMo-V2.5 官方模型页](https://mimo.mi.com/models/mimo-v2.5)
- 开放平台普通按量付费 Key 使用 `https://api.xiaomimimo.com/v1`。按量付费消耗账户余额，与 Token Plan 套餐额度不互通；本项目是自定义应用后端，应使用普通按量付费 API，而不是仅面向编程工具套餐的 Token Plan。[MiMo 按量付费价格](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)、[MiMo Token Plan](https://mimo.mi.com/docs/en-US/price/token-plan)

### 官方公开价格

| 计费区域 | 币种与单位 | 普通输入（缓存未命中） | 输入（缓存命中） | 输出 |
| --- | --- | ---: | ---: | ---: |
| 国内 | 人民币元 / 百万 Token | ¥1.00 | ¥0.02 | ¥2.00 |
| 海外 | 美元 / 百万 Token | $0.14 | $0.0028 | $0.28 |

来源：[MiMo 按量付费价格](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)、[MiMo-V2.5 官方模型页](https://mimo.mi.com/models/mimo-v2.5)

官方计费说明还明确：

- 请求前缀命中 Prompt Cache 时，命中部分按缓存价计费。
- 缓存写入目前“限时免费”。
- 联网搜索按调用次数另行计费，不包含在模型 Token 单价中。本项目分类请求不应把联网搜索费混入模型价格估算。[MiMo 按量付费价格](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)

### 思考 Token 如何计费

- MiMo 没有公布一套独立的 reasoning 单价。OpenAI Chat API 的 `usage.completion_tokens` 是生成 completion 的 Token 总数，`completion_tokens_details.reasoning_tokens` 只是其中思考 Token 的拆分；Responses API 同样把 `reasoning_tokens` 放在 `output_tokens_details` 下。[MiMo OpenAI Chat API](https://mimo.mi.com/docs/en-US/api/chat/openai-api)、[MiMo Responses API](https://mimo.mi.com/docs/en-US/api/chat/responses)
- 结合价格页仅公布“输出”单价，可以确定思考 Token 包含在输出 Token 中，按同一输出价计费，而不是另设费率。这一结论来自官方 usage 字段结构与官方价格表的对应关系。

### 降价、促销与有效期

- MiMo 官方宣布 V2.5 系列按量 API 永久降价，新价格自 2026-05-27 00:00（北京时间）生效，并且不再按输入长度分档。当前上表即降价后的公开价格。[MiMo-V2.5 价格调整公告](https://mimo.mi.com/docs/en-US/news/v2.5-price-update)
- “缓存写入限时免费”没有公布结束日期，因此只能标记为截至 2026-08-29 仍有效、结束时间未公开，不能把它写成永久政策。[MiMo 按量付费价格](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)
- Token Plan 的首购、续费、夜间系数等优惠不适用于普通按量付费 Key，也不应进入 SciRSSAgent 的按量成本计算。[MiMo Token Plan](https://mimo.mi.com/docs/en-US/price/token-plan)

## 3. 对 SciRSSAgent 价格配置的直接建议

1. Qwen 使用北京共享端点时，价格快照应写为：缓存命中输入 `0.1`、普通输入 `0.8`、输出 `2.7`，单位均为人民币元 / 百万 Token，模型 ID 为 `qwen3.8-flash`。
2. MiMo 普通中国大陆按量 Key 使用：缓存命中输入 `0.02`、普通输入 `1.00`、输出 `2.00`，单位均为人民币元 / 百万 Token，模型 ID 为 `mimo-v2.5`。
3. 两者的 reasoning/thinking Token 都按输出价计入；分类请求显式关闭 thinking 后，通常不会产生这部分额外输出成本。
4. 价格变量或成本报告应注明快照日期 `2026-08-29`，不要把 Qwen 控制台活动折扣、Qwen 90 天免费额度或 MiMo 限时免费缓存写入当作长期单价。
5. 如果用户实际持有国际版 Qwen Key 或海外 MiMo 账户，应切换至对应区域价格，不能继续使用上述人民币国内价格。
