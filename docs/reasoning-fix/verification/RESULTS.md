# 验证记录

基线：Wei-Shaw/sub2api `69d2fae91318359e4e25c0171eaf70364dad3629`。测试使用 Go 1.27.4 Alpine 容器，在 Linux arm64 环境执行；镜像目标平台为 Linux amd64。

## 自动化回归

| 范围 | 顶层测试 | 含子测试 | 结果 |
| --- | ---: | ---: | --- |
| `internal/pkg/apicompat` 全包 | 505 | 712 | 通过 |
| `internal/service` 下列定向匹配 | 59 | 158 | 通过 |

```sh
go test -json -tags=unit ./internal/pkg/apicompat -count=1
go test -json -tags=unit ./internal/service -run 'TestForwardAsChatCompletions|TestChatReasoning|TestNormalizeOpenAIAPIKeyStoreFalseReasoningReplay|TestOmitSyntheticChatReasoningForNativeResponses' -count=1 -timeout=240s
```

这并非整个后端所有测试。日志见 `apicompat-tests.jsonl`、`service-tests.jsonl`；统计见 `test-summary.json`。未改依赖版本，未调用收费模型接口。

覆盖：独立 reasoning 历史、reasoning alias 与字段优先级、结构化 thinking、两轮工具重放、流式/非流式 reasoning-only、正常空响应、真实正文与字面标签、工具调用 ID/参数/结果、usage 尾块、stop/length、finalize 幂等、Claude 不伪造签名，以及六类官方/第三方目标的服务出站请求捕获。

## 构建与包校验

- 原上游 Dockerfile 完整多阶段构建通过，包含前端类型检查/打包及带 embed 的后端 Linux amd64 编译；日志为 `docker-build-linux-amd64.log`。
- 本地镜像：`sub2api-reasoning-fix:69d2fae-20261118`，版本 `0.2.22-reasoningfix.20261118`。
- 镜像 ID / 本地清单摘要：`sha256:2eb562de146946034277d32c71c7441535b95fc8c21bc0dff0d3d0e64b0dbe47`；平台 `linux/amd64`，Docker 报告大小 47241725 bytes。
- `--version` 启动检查通过；隔离 setup 模式的状态端点、嵌入前端及导出后 docker load 结果见 `image-smoke.log`。setup 检查不等于配置数据库后的生产健康验收。
- 未连接生产配置，未部署或推送到外部镜像仓库；ARM64 镜像未构建。

最终补丁已对基线执行 `git apply --cached --check`，并将应用后每个源文件的 Git 内容哈希与打包源文件逐一比较；具体树 ID 和补丁 SHA256 见 `package-check.json`。ZIP CRC、路径与 SHA256 清单同时校验。包内不含 `.git`、依赖缓存、实际 `.env` 或部署凭据；上游 `.env.example` 等公开模板保留。

## 验证范围

没有客户项目、配置或原始故障日志，尚未确认客户混乱输出的实际根因，也未在客户 Harness/飞书 Bot、真实官方账号、第三方上游或生产数据库上测试。服务层出站回归使用本地模拟响应。

本修复隔离转换过程中的推理与正文，保留独立 reasoning/thinking 字段。它不会清洗普通 content 中的字面标签，不能阻止 Bot 自行拼接所有字段，也没有实现 Claude 原生签名 thinking 的完整回放。reasoning-only 不会再伪装成最终答案；调用方应处理没有最终正文或 token 截断的情况。现场验收见 `ACCEPTANCE.md`。
