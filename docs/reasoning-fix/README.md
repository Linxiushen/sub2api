# Sub2API reasoning 兼容修复版

供运行多套 Harness、统一经 Sub2API 分发模型 API 的团队试用。交付目标是在 Sub2API 的协议转换处修复已确认的 reasoning 混入正文问题；现有 Harness 和飞书 Bot 继续连接原来的服务地址，无需另加一层 Harness。

本包是基于公开源码的第三方修复，不是上游官方发行版，也没有在客户真实环境中验证。请先在测试实例验收，再安排生产切换。

## 下载与快速使用

- [下载完整修复包或源码补丁包](https://github.com/Linxiushen/sub2api-reasoning-fix/releases/tag/reasoning-fix-20261118)。含镜像包为 Linux amd64。
- 在本仓库根目录运行 `bash docs/reasoning-fix/build.sh` 可重新构建；验收可运行 `bash docs/reasoning-fix/verify-tests.sh`。
- 本目录保留原交付包说明。下文中的“包目录”指 Release 解压后的根目录，源码目录为其中的 `sub2api/`；本仓库的源码则直接位于仓库根目录。
- `verification/package-check.json` 校验的是 Release 原始交付包的源码树；本次仓库额外加入的文档入口不属于该补丁树。

## 来源与范围

- 上游仓库：<https://github.com/Wei-Shaw/sub2api>
- 基线提交：`69d2fae91318359e4e25c0171eaf70364dad3629`
- 本地镜像标签：`sub2api-reasoning-fix:69d2fae-20261118`
- 构建方式：沿用仓库原有 `Dockerfile`，默认目标平台 `linux/amd64`。
- Go 依赖来源：`https://proxy.golang.org,direct`，校验服务 `sum.golang.org`。
- 发布前验证：兼容层全量 **505 项**顶层测试、服务层定向 **59 项**顶层回归通过（含子测试共 870 项）。详细命令、范围和日志见 `verification/RESULTS.md`。

本次修复保持结构化字段分离：上游 Chat Completions 的 `reasoning_content` / `reasoning` 映射到 Responses 的 reasoning item 或 Anthropic 的 thinking block，不再在没有最终答案时复制成可见正文；历史协议转换同样避免把结构化 reasoning 包装进正文。发往官方 OpenAI/Codex 的 Chat→Responses 请求会省略无法原样重放的合成明文 reasoning 历史；第三方兼容服务保留独立 summary。原生 Responses 入站不受这一专用过滤影响。

当上游只返回 reasoning 时，修复后的接口仍保留 reasoning、用量与结束状态，但没有最终正文。若是 token 上限导致中断，保留对应 incomplete / max_tokens 状态。工具调用和真实正文继续按原协议转换。正常正文里本来就有的 `<analysis>`、`<think>` 等字面字符串保持原样，本包不做正则删除。

以下情况需要单独处理：上游把思考内容直接写进普通 `content`；Harness 或 Bot 主动把 reasoning/thinking 展示为最终答案；其他未覆盖的协议路径；Claude 原生带签名 thinking 的完整重放。本包没有承诺修复这些情况，也不等同于复现了客户的根因。

## 包内内容

- `sub2api/`：修复后的源码，保留上游许可证。
- `build.sh`：只构建本地镜像，不替换正在运行的服务。
- `compose.reasoning-fix.example.yml`：仅覆盖已有应用服务的 image。
- `ROLLBACK.md`：升级前备份与回滚步骤。
- `ACCEPTANCE.md`：接口和真实 Harness / Bot 验收清单。
- `reasoning-isolation.patch`：针对上述基线的源码和测试补丁。
- `verification/`、`SHA256SUMS`：测试、镜像、来源记录及文件校验。
- 完整包额外包含 `sub2api-reasoning-fix-linux-amd64.tar.gz`；源码包不含镜像。

## 最少操作流程

1. 运维确认现有 Sub2API 版本、CPU 架构、Compose 文件路径和**应用服务名**；记录当前运行镜像 ID。示例服务名为 `sub2api`，不一定与客户一致。
2. 按 [ROLLBACK.md](ROLLBACK.md) 保存数据库升级前快照、旧镜像、配置与数据卷备份，并验证恢复路径。先在独立测试数据库上跑新版本。
3. 如果下载的是含镜像的完整包，且目标为 Linux amd64，先加载：

   ```sh
   docker load -i sub2api-reasoning-fix-linux-amd64.tar.gz
   ```

   源码包或其他架构则在包目录构建：

   ```sh
   bash ./build.sh
   ```

   若服务主机为 ARM64：

   ```sh
   PLATFORM=linux/arm64 bash ./build.sh
   ```

   若源码位于其他目录：

   ```sh
   SOURCE_DIR=/absolute/path/to/sub2api bash ./build.sh
   ```

4. 把示例 override 复制到**现有部署目录**，确认其中服务名和旧配置的应用服务名一致。保持原有端口、数据库、Redis、环境变量、密钥、数据卷、网络和上游账号配置。不要拿源码中的完整 Compose 文件覆盖客户现有部署。
5. 先在测试实例使用这个镜像，按 [ACCEPTANCE.md](ACCEPTANCE.md) 验收。生产切换前确认数据库备份完成、测试通过，并安排可回滚的维护窗口。
6. 生产切换仅替换应用服务。下列命令是运维人工执行示例，请先把 `compose.yml` 和服务名换成实际值：

   ```sh
   docker compose -f compose.yml -f compose.reasoning-fix.yml config --services
   docker compose -f compose.yml -f compose.reasoning-fix.yml config --images
   docker image inspect sub2api-reasoning-fix:69d2fae-20261118 --format '{{.Id}}'
   docker compose -f compose.yml -f compose.reasoning-fix.yml up -d --no-deps --no-build --pull never sub2api
   docker compose -f compose.yml -f compose.reasoning-fix.yml ps sub2api
   docker compose -f compose.yml -f compose.reasoning-fix.yml logs --tail 100 sub2api
   ```

   上面不会创建一套新数据库。**但新应用启动本身可能执行数据库迁移**：本包基线与客户当前版本可能相隔较大，`--no-deps` 并不阻止迁移。没有升级前备份时不要进行生产切换。

7. 用原来的 API 地址、原模型路由和各 Harness / 飞书 Bot 实测。修复在 Sub2API 处集中生效，仍须客户确认每种实际接入协议的结果。保留脱敏请求 ID、时间、协议与最终展示结果；不要把 API Key、Cookie 或用户隐私写进交付报告。

若镜像在别的机器构建，可用 `docker save` / `docker load` 转运；构建平台必须与服务主机匹配。示例 override 的 `pull_policy: never` 要求目标机器已经有这个本地镜像，避免误拉同名远端镜像。

## 使用补丁而不是升级整套版本

若客户版本与本包基线不同，优先让其维护者评估将补丁合入现有版本，重新执行测试和构建。不要直接覆盖客户的未知版本代码，也不要用跳过迁移、关闭数据校验来强行启动。

如客户验收仍失败，最少补充：Sub2API 版本/镜像 ID、接入端点、模型映射、是否流式、一条脱敏请求与对应原始响应片段、最终 Bot 展示截图，以及关联请求 ID。结构化 reasoning 正常而 Bot 仍显示混乱时，再检查其接入和渲染路径。
