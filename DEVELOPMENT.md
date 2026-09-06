# 开发与验收

本文档记录当前实现范围、工程约束、验证入口和剩余工作。产品使用方法见 [docs/USAGE.md](docs/USAGE.md)，权限与数据流见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 当前状态

项目已经具备可构建的 React + Go 应用、非 root Web 服务、root Helper、SQLite、Avahi D-Bus 集成、配置安全写入、审计、快照和 systemd 安装脚本。GitHub Actions 可根据 `v*` 标签发布 Linux AMD64 安装包。

仍属于开发版本，尚未完成真实 Linux systemd 环境的完整生产验收、Debian 包、APT 更新流程及整个 `/etc/avahi` 辅助文件范围的备份。

## 固定设计约束

- `/etc/avahi` 始终是 Avahi 配置的事实来源，SQLite 不保存配置副本作为主数据。
- Web 进程必须以非 root 身份运行，不能直接写 `/etc/avahi` 或控制 systemd。
- Helper 必须以 root 身份运行，只接受固定 RPC，并使用 `SO_PEERCRED` 校验 Web 进程 UID。
- 默认仅监听 `127.0.0.1:8053`；改变访问地址时必须同步设置精确 Origin。
- 配置修改必须经过版本检查、快照、原子写入、Reload/Restart、健康检查和失败回滚。
- 停止或卸载 Manager 不能删除或破坏当前 Avahi 配置。
- 不自行实现 mDNS、DNS Server、DHCP、集群、云管理、LDAP、OAuth、复杂 RBAC、插件系统或自定义 reflector。
- 不提供 Docker/Kubernetes 部署；目标是原生 Linux systemd。

## 模块状态

| 模块 | 已实现 | 仍需完成 |
| --- | --- | --- |
| 检测 | OS、Avahi binary、systemd service/socket、D-Bus、配置文件检测 | 缺少 Avahi 时的产品化引导 |
| 配置 | daemon、hosts、service 解析/校验，原子写入，外部漂移检测 | 扩大发行版配置兼容样本，完整文件 diff |
| Safe Apply | 快照、baseline、健康检查、失败回滚、崩溃恢复、保留策略 | 完整 `/etc/avahi/*` 辅助文件归档 |
| systemd | 状态读取及 Start/Stop/Restart/Reload/Enable/Disable | 真实隔离环境的写操作验收 |
| Avahi D-Bus | 状态、发现、Resolve、重连和事件缓存 | 多接口高负载与长期运行验证 |
| 数据库与认证 | SQLite WAL/迁移、Argon2id、会话、限流、审计和任务表 | 更新任务生命周期接线 |
| API 与 UI | 八页 React UI、REST API、SSE、配置/服务/主机/日志/快照 | 更新 UI、异常状态与完整差异展示 |
| 部署 | 两文件发布目录、安装/更新/卸载、账户、权限、tmpfiles、systemd | Debian 包和真实发行版生产验收 |
| 发布 | `v*` 标签自动生成 Linux AMD64 Release | 在仓库启用后完成首次标签发版验证 |

## 测试覆盖

仓库测试覆盖以下关键路径：

- 配置解析、原子替换、符号链接拒绝、版本冲突、快照篡改、回滚和崩溃恢复
- systemd 精确 unit/argv、任务完成、失败与超时
- 私有 D-Bus 上的状态、发现、Resolve、owner 丢失与取消
- fsnotify rename 保存和 services 目录重建
- SQLite 迁移、WAL、权限、审计和并发写入
- Argon2id、单管理员、Cookie 会话、注销、过期和登录限流
- Helper Unix socket、双向 UID 校验、固定 RPC 和非法路径拒绝
- API 的认证、CSRF、Origin/Host、配置 CRUD、审计和 SSE
- Chromium 端到端的登录、服务、主机、设置、发现、备份、审计和移动布局

这些测试主要使用临时目录、私有 D-Bus 和模拟 systemd，不应修改开发机上的 Avahi 配置。

## 验证命令

以下命令必须在 Linux 上执行：

```bash
make check
make e2e
make dist
bash -n packaging/install.sh
```

发布前还应在干净的 Debian/Ubuntu systemd 虚拟机中完成：

1. 首次安装和管理员初始化。
2. 重启系统后 socket、Helper 与 Web 自动恢复。
3. Web 中执行配置修改、快照恢复和 Avahi 生命周期操作。
4. 重复安装完成原地升级且数据保留。
5. 普通卸载保留数据，`--purge` 删除数据和脚本创建的账户。

## 后续优先级

1. 真实 Linux systemd 安装、重启、升级和卸载验收。
2. 完整配置目录备份边界和恢复异常处理。
3. APT 更新检查、确认、任务审计及 UI。
4. 发行版配置兼容性和异常状态展示。
5. Debian 包与持续发布验证。
