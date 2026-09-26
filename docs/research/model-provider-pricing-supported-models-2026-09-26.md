# 已支持模型的价格核验（MiMo V2.6）

核验日期：2026-09-26

范围：当前目录中用于分类和 Profile 的实时、按量 API 价格；不含 Token Plan、Batch、账户赠额或其他限时折扣。表中每项均为人民币元 / 百万 Token，顺序为缓存命中输入 / 缓存未命中输入 / 输出。价格快照供应用 usage 估算使用，最终账单以供应商控制台为准。

## 当前费率快照

| Model ID | 缓存命中 | 输入未命中 | 输出 | 时段 |
| --- | ---: | ---: | ---: | --- |
| `deepseek-flash` | ¥0.02 / ¥0.04 | ¥1 / ¥2 | ¥4 / ¥8 | 空闲 / 高峰 |
| `deepseek-v4-pro` | ¥0.15 / ¥0.30 | ¥4.5 / ¥9 | ¥13.5 / ¥27 | 空闲 / 高峰 |
| `glm-5.3-flash` | ¥0.23 | ¥0.8 | ¥2.8 | 标准价 |
| `glm-5.3` | ¥2 | ¥8 | ¥28 | 标准价 |
| `qwen3.8-flash` | ¥0.1 | ¥0.8 | ¥2.7 | 中国大陆百炼 |
| `qwen3.8-max-0902` | ¥1.2* | ¥12 | ¥36 | 中国大陆百炼 |
| `mimo-v2.6-flash` | ¥0.02 | ¥1 | ¥2 | 实时 API |
| `mimo-v2.6-pro` | ¥0.025 | ¥3 | ¥6 | 实时 API |

DeepSeek 表格中的双值依次表示空闲 / 高峰。高峰时段为北京时间周一至周五 09:00–12:00、14:00–18:00（不含中国法定节假日）；其余时段使用空闲价格。应用已使用 2026-09-10 的现行 DeepSeek 费率。Qwen Max 的缓存命中 ¥1.2 是应用当前按 10% 输入价估算的缓存费率；百炼价目将其标为上下文缓存折扣，实际折扣以控制台账单为准。

价格来源：[DeepSeek 官方价格表](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)、[MiMo V2.6 官方价格表](https://mimo.mi.com/docs/en-US/pricing)、[Qwen3.8-Flash 官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)、[百炼模型价格](https://help.aliyun.com/zh/model-studio/model-pricing)、[智谱 BigModel 定价页](https://bigmodel.cn/pricing)。

## MiMo V2.6 的模型选择

MiMo 官方现价为 V2.6 Flash ¥0.02/¥1/¥2，V2.6 Pro ¥0.025/¥3/¥6。该价格与 V2.5 对应型号一致；官方 V2.6 价格页在 2026-09-22 更新。两者均可使用普通按量付费 Key 和 `https://api.xiaomimimo.com/v1` 的 OpenAI 兼容接口；Token Plan Key 与按量端点的凭据及计费不同。[MiMo Flash](https://mimo.mi.com/models/en-US/mimo-v2.6-flash)、[MiMo Pro](https://mimo.mi.com/models/en-US/mimo-v2.6-pro)、[MiMo API 定价](https://mimo.mi.com/docs/en-US/pricing)。

V2.6 Pro 的缓存未命中输入价是 DeepSeek Flash 空闲时段的 3 倍（¥3 对 ¥1），高峰时段也高 50%（¥3 对 ¥2）。现行策略让分类和 Profile 共用完整的八模型目录，用户可按质量与成本自行选择各角色默认模型；该对比保留为选型参考，不再限制 V2.6 Pro 的角色范围。

## 其他已支持模型的价格变化

- DeepSeek Flash 已按 2026-09-10 调价后的 ¥0.02/¥1/¥4 空闲标准价、峰时翻倍计价；代码快照已是现价。
- GLM-5.3-Flash 的 50% 促销已于 2026-09-10 结束，代码使用 ¥0.23/¥0.8/¥2.8 标准价。GLM-5.3 旗舰费率为 ¥2/¥8/¥28；本次未发现后续调价。智谱价格页由客户端脚本渲染，浏览器静态抓取无法读取表格，费率沿用应用已核对的标准价快照。
- Qwen3.8-Flash 当前中国大陆标准价仍为 ¥0.1/¥0.8/¥2.7；Qwen3.8-Max-0902 的 ¥12 输入、¥36 输出标准价也未见变化。
- MiMo V2.6 沿用 V2.5 同档费率，无需改动费率数值；应用为新模型 ID 单独写入 V2.6 价格快照。

## 接口兼容核对

MiMo V2.6 官方模型页列出 OpenAI 与 Anthropic 协议兼容，并给出 `https://api.xiaomimimo.com/v1`、`MIMO_API_KEY`、新 Model ID、`max_completion_tokens` 及 `thinking.type` 请求示例。Flash 与 Pro 均支持结构化输出。V2.6 上线后，项目通过既有 classifier/profile 请求适配器和配置的 MiMo Key 各完成过一次连接请求；无需新增供应商适配层。[Flash 接口示例](https://mimo.mi.com/models/en-US/mimo-v2.6-flash)、[Pro 接口示例](https://mimo.mi.com/models/en-US/mimo-v2.6-pro)。

旧配置中的 `mimo-v2.5` 和 `mimo-v2.5-pro` 分别解析为 V2.6 Flash 和 Pro，以免已有本地设置在版本切换后丢失。它们不再作为可选目录项，也不会向供应商发送 V2.5 Model ID；V2.5 费率代码仅用于仍保留旧 Model ID 的历史 usage 记录。
