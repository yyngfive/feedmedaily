# Qwen3.8-Flash 与 MiMo-V2.5 分类模型接入核验

核验日期：2026-08-29

范围：只核验官方模型 ID、OpenAI 兼容端点、API Key 命名惯例，以及 Chat Completions 下 thinking 与结构化输出的关系。本笔记不记录或展示本机 `.env` 中的密钥值。

## 结论摘要

| 项目                                 | Qwen3.8-Flash                                                                                                                       | MiMo-V2.5                             |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| API`model` ID                      | `qwen3.8-flash`                                                                                                                   | `mimo-v2.5`                         |
| OpenAI 兼容 Base URL（按量付费常用） | 中国大陆：`https://dashscope.aliyuncs.com/compatible-mode/v1`；国际版：`https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `https://api.xiaomimimo.com/v1`     |
| 官方环境变量惯例                     | `DASHSCOPE_API_KEY`                                                                                                               | `MIMO_API_KEY`                      |
| 本项目可采用的供应商变量             | 用户已有`QWEN_API_KEY`，可以作为项目自定义名称读取                                                                                | 使用`MIMO_API_KEY`                  |
| Thinking 默认值                      | 默认开启，可关闭                                                                                                                    | 默认开启，可关闭                      |
| Chat Completions 控制形状            | 顶层`enable_thinking: false`，或 Qwen3.8 的 `reasoning_effort: "none"`                                                          | 顶层`thinking: {"type":"disabled"}` |
| 当前分类 JSON 模式建议               | 显式关闭 thinking                                                                                                                   | 显式关闭 thinking                     |

## 1. Qwen3.8-Flash

### 模型 ID 与端点

- 官方产品名为 Qwen3.8-Flash，API 的准确 `model` 参数是小写的 `qwen3.8-flash`。用户写的 `QWEN-3.8-flash` 可视为展示写法，不应原样作为请求 ID。[官方模型页](https://help.aliyun.com/zh/model-studio/qwen3-8-flash)
- 百炼 OpenAI 兼容共享端点为：中国大陆 `https://dashscope.aliyuncs.com/compatible-mode/v1`，国际版 `https://dashscope-intl.aliyuncs.com/compatible-mode/v1`。API Key 必须与对应地域匹配。生产环境也可使用业务空间专属域名。[获取与配置 API Key](https://help.aliyun.com/zh/model-studio/get-api-key/)、[OpenAI 兼容 Base URL](https://help.aliyun.com/en/model-studio/base-url)
- 阿里云示例统一使用 `DASHSCOPE_API_KEY`。`QWEN_API_KEY` 不是百炼官方变量名，但环境变量名由应用自行读取，因此 SciRSSAgent 可以把用户现有的 `QWEN_API_KEY` 定义为项目自己的供应商密钥名。[获取与配置 API Key](https://help.aliyun.com/zh/model-studio/get-api-key/)

### Thinking 要求

- `qwen3.8-flash` 是混合思考模型，默认开启思考；不是“必须开启”的纯思考模型，可以显式关闭。[深度思考模型的用法](https://help.aliyun.com/zh/model-studio/deep-thinking)
- OpenAI Chat Completions 请求可使用非标准顶层参数 `enable_thinking: true|false`。使用 OpenAI Python SDK 时，官方要求放在 `extra_body` 中；SciRSSAgent 直接构造 HTTP JSON 请求时，应把它放在请求体顶层。[OpenAI 兼容 Chat API](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)
- Qwen3.8 也支持 `reasoning_effort`。官方档位为 `low`、`medium`、`xhigh`，并接受别名映射；`none` 映射为 `enable_thinking=false`。`reasoning_effort` 与 `thinking_budget` 不可同时设置，否则会报错。对于只需要开/关的分类适配器，直接用 `enable_thinking:false` 最清晰；若项目已统一使用 reasoning effort，则 `reasoning_effort:"none"` 等价可用。[OpenAI 兼容 Chat API](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)
- 开启后，思考内容位于 `reasoning_content`，最终答案位于 `content`。Qwen3.8 默认 `preserve_thinking=true`；多轮请求若回传历史思考，应完整保留 `reasoning_content`，不能拼入 `content`。当前分类为独立请求，不需要依赖历史思考。[OpenAI 兼容 Chat API](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)

### 与结构化输出的关系

- `qwen3.8-flash` 官方同时列入 JSON Object 和 JSON Schema 支持列表，而且不像 Qwen3.6/Qwen3.5 Flash 那样标注“仅非思考模式”，因此 Qwen3.8-Flash 的结构化输出本身不要求关闭 thinking。[结构化输出](https://help.aliyun.com/zh/model-studio/qwen-structured-output)
- JSON Object 请求形状为 `response_format:{"type":"json_object"}`，且 system 或 user 消息中必须出现不区分大小写的 `JSON` 关键词；否则会报错。JSON Schema 可用 `response_format:{"type":"json_schema",...}`，建议启用 `strict:true`。[结构化输出](https://help.aliyun.com/zh/model-studio/qwen-structured-output)
- 对 SciRSSAgent 分类任务，仍建议显式关闭 thinking：分类结果是固定 JSON，关闭可减少推理 token、延迟和 `reasoning_content` 处理，同时保留 `response_format:{"type":"json_object"}`。这是任务适配建议，不是 Qwen3.8 的强制限制。

## 2. MiMo-V2.5

### 模型 ID 与端点

- 官方模型 ID 是 `mimo-v2.5`。该模型支持文本、图像、视频、音频输入，文本输出，并支持结构化输出。[MiMo-V2.5 官方模型页](https://mimo.mi.com/models/mimo-v2.5)
- 按量付费 OpenAI 兼容 Base URL 是 `https://api.xiaomimimo.com/v1`。Token Plan 使用控制台给出的地域专属 URL，例如 `https://token-plan-{region}.xiaomimimo.com/v1`，不可与按量付费 Key 混用。[官方 Oh My Pi 接入指南](https://github.com/XiaomiMiMo/awesome-mimo-agent/blob/main/docs/oh-my-pi.zh-CN.md)、[错误码说明](https://mimo.mi.com/docs/en-US/api/guidance/error-codes)
- 小米官方文档和官方 GitHub 示例均使用 `MIMO_API_KEY`。[官方 Oh My Pi 接入指南](https://github.com/XiaomiMiMo/awesome-mimo-agent/blob/main/docs/oh-my-pi.zh-CN.md)、[结构化输出示例](https://mimo.mi.com/docs/en-US/quick-start/usage-guide/text-generation/structured-output)

### Thinking 要求

- `mimo-v2.5` 是可开关的混合思考模型，默认开启；不是必须开启。Chat Completions 使用 `thinking.type` 控制，可选 `enabled`、`disabled`。[深度思考](https://mimo.mi.com/docs/en-US/usage-guide/passing-back-reasoning_content)、[OpenAI Chat API](https://mimo.mi.com/docs/en-US/api/chat/openai-api)
- OpenAI Python SDK 的请求形状为 `extra_body={"thinking":{"type":"disabled"}}`；直接 HTTP JSON 请求中对应顶层 `thinking:{"type":"disabled"}`。[深度思考](https://mimo.mi.com/docs/en-US/usage-guide/passing-back-reasoning_content)
- 开启 thinking 时，`temperature` 和 `top_p` 即使传入也会被强制为官方推荐值 `1.0`、`0.95`。`max_completion_tokens` 同时限制思考和最终答案长度，过小可能挤压最终输出。[深度思考](https://mimo.mi.com/docs/en-US/usage-guide/passing-back-reasoning_content)
- 开启 thinking 的多轮工具调用必须完整回传历史 assistant 消息的 `reasoning_content`，否则 API 会返回 400。当前分类路径没有多轮工具调用，因此不涉及这一要求。[深度思考](https://mimo.mi.com/docs/en-US/usage-guide/passing-back-reasoning_content)、[错误码说明](https://mimo.mi.com/docs/en-US/api/guidance/error-codes)

### 与结构化输出的关系

- 官方结构化输出目前是 JSON Object 模式：`response_format:{"type":"json_object"}`；支持 `mimo-v2.5-pro` 与 `mimo-v2.5`。[结构化输出](https://mimo.mi.com/docs/en-US/quick-start/usage-guide/text-generation/structured-output)
- 提示词必须明确要求只返回 JSON，并完整定义字段、层级和数据类型；官方建议提供示例。流式模式下需先拼接完整字符串再解析，并给足 `max_completion_tokens`，避免 JSON 被截断。[结构化输出](https://mimo.mi.com/docs/en-US/quick-start/usage-guide/text-generation/structured-output)
- 官方未声明 MiMo 的 JSON Object 与 thinking 互斥，两项能力由同一模型支持。因此“结构化输出必须关闭 thinking”不是官方限制。不过分类任务无需深度推理，建议显式发送 `thinking:{"type":"disabled"}`，以减少延迟和费用，并让 `content` 中的 JSON 更直接。

## 3. 给 SciRSSAgent 实现的直接建议

1. 模型 ID 使用 `qwen3.8-flash` 和 `mimo-v2.5`。
2. 若用户的 Qwen Key 属于中国大陆百炼，使用 `https://dashscope.aliyuncs.com/compatible-mode/v1`；若属于国际版，必须改用国际端点。不能只凭变量名判断地域。
3. 项目可以采用 `QWEN_API_KEY` 作为自己的配置名；文档中应注明百炼官方惯例是 `DASHSCOPE_API_KEY`。
4. MiMo 配置名采用 `MIMO_API_KEY`
5. 分类请求继续使用 `response_format:{"type":"json_object"}`，并为两个模型都显式关闭 thinking：
6. - Qwen：`enable_thinking:false`（或现有适配层使用等价的 `reasoning_effort:"none"`）。
   - MiMo：`thinking:{"type":"disabled"}`。
7. Qwen 的分类提示词中确认含有 `JSON` 关键词；MiMo 的提示词中明确“只返回 JSON”并描述完整结构。
