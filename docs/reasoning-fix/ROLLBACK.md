# 升级前备份与回滚

本包来自公开仓库的新基线，客户当前版本未知。应用首次启动可能自动迁移数据库；**回退旧镜像不一定能回退数据库结构**。上线前必须具备与旧镜像配套的数据库升级前快照和实际可用的恢复步骤。

## 生产切换前

1. 记录当前 Sub2API 版本、容器镜像 ID、实际 Compose 服务名、数据库版本和部署拓扑。保留原 Compose、`.env`、应用配置、数据卷，以及恢复所需加密密钥；这些均留在客户自己的受限备份位置，不放入修复包。
2. 为正在运行的应用镜像创建本地保留标签，并导出。示例假定 Compose 文件为 `compose.yml`，服务名为 `sub2api`，应先改成实际值：

   ```sh
   app_container_id="$(docker compose -f compose.yml ps -q sub2api)"
   test -n "$app_container_id"
   old_image_id="$(docker inspect "$app_container_id" --format '{{.Image}}')"
   docker tag "$old_image_id" sub2api-pre-reasoning-fix:20261118
   docker image save -o sub2api-pre-reasoning-fix-20261118.tar sub2api-pre-reasoning-fix:20261118
   ```

3. 使用客户现有的数据库快照/备份方案，制作升级前备份，并在独立数据库验证能恢复。检查账号、路由、配额和关键数据可读取。数据库原生快照应按其一致性要求执行；不要只复制正在写入的数据库文件。
4. 用独立测试数据库和测试资源运行新镜像，验证迁移、登录、上游路由、计量及本包验收项。测试实例不得连接生产数据库；多个实例共用生产数据库不能隔离迁移风险。
5. 上线维护窗口先停止新流量并处理在途请求，制作最终一致性备份，再替换应用镜像。记下备份时间点及上线后新增写入的处理办法。

## 出现异常时

1. 停止新流量，暂停新版本应用，保存脱敏错误、请求 ID 和迁移日志。不要反复启动旧应用去尝试兼容已迁移的数据库。
2. 如旧镜像不在本机，加载之前保存的镜像：

   ```sh
   docker image load -i sub2api-pre-reasoning-fix-20261118.tar
   ```

3. 若新应用已执行迁移或有其他不兼容数据库写入，按已验证的运维流程恢复**升级前数据库快照**和配套配置/数据。恢复会影响快照后新增数据，应先留存并由客户决定补录/对账方式。只有确认未迁移且数据库仍与旧版兼容，才可评估仅回退镜像。
4. 创建临时 rollback override，仅写旧镜像与实际应用服务名：

   ```yaml
   services:
     sub2api:
       image: sub2api-pre-reasoning-fix:20261118
       pull_policy: never
   ```

5. 在数据库和配置恢复完成后，人工以**原 Compose + rollback override**启动应用；不要再叠加 reasoning-fix override。示例：

   ```sh
   docker compose -f compose.yml -f compose.rollback.yml up -d --no-deps --no-build --pull never sub2api
   docker compose -f compose.yml -f compose.rollback.yml ps sub2api
   ```

6. 验证健康、管理登录、路由、计量和一条真实业务请求后再恢复流量。保留旧镜像和备份，直到修复版验收稳定。
