# 架构与安全边界

## 项目定位

Avahi Manager 管理现有 Avahi，不实现或替代 mDNS/DNS-SD。Avahi 原生配置、D-Bus 和 systemd 状态是运行事实；SQLite 只保存管理器自身的数据。

## 进程边界

```text
Browser
   │ HTTP / SSE
   ▼
avahi-manager serve                 非 root 服务账户
   ├── SQLite                       管理员、会话、审计和状态
   ├── Avahi D-Bus                  状态、发现和 Resolve
   ├── journalctl                   Avahi 日志读取
   └── Unix socket RPC
           │ SO_PEERCRED 双向 UID 校验
           ▼
avahi-manager helper                root
   ├── /etc/avahi                   受控配置读写
   ├── /var/lib/avahi-manager-helper 快照和 baseline
   └── systemd D-Bus                固定 Avahi 生命周期操作
```

`serve` 会拒绝 root 运行；`helper` 会拒绝非 root 运行。Helper 只接受安装时指定的 Manager UID，并且 RPC 方法、参数和可操作的 systemd unit 都是白名单。

## 数据来源与职责

| 数据 | 事实来源 | 写入者 |
| --- | --- | --- |
| Avahi daemon 配置 | `/etc/avahi/avahi-daemon.conf` | root Helper |
| 静态 hosts | `/etc/avahi/hosts` | root Helper |
| 静态服务 | `/etc/avahi/services/*.service` | root Helper |
| Avahi 运行状态 | systemd 与 Avahi D-Bus | Avahi/systemd |
| 服务发现缓存 | `serve` 内存 | D-Bus 事件处理器 |
| 管理员和会话 | SQLite | 非 root Web |
| 审计、任务和漂移 | SQLite | 非 root Web |
| 快照和 baseline | Helper 状态目录 | root Helper |

SQLite 不能覆盖原生配置，也不能在启动时把旧数据库状态写回 `/etc/avahi`。

## 配置修改事务

一次修改遵循以下顺序：

1. Web 提交读取时获得的 revision。
2. Helper 重新读取磁盘并检查 revision，拒绝并发或外部修改冲突。
3. 创建修改前快照和 pending 事务记录。
4. 校验并通过临时文件、fsync 和原子 rename 写入。
5. 根据变更类型调用 Reload 或 Restart。
6. 检查 systemd 和 Avahi D-Bus 健康状态。
7. 成功后更新 baseline；失败时恢复快照并再次应用。

Helper 启动时会检查未完成事务，避免进程中断后留下半完成配置。普通手动快照不会自动改变 baseline。

## 权限模型

- Web 数据目录只允许服务账户访问。
- Helper 状态和备份目录为 `root:root 0700`。
- socket 目录为 `root:服务组 0750`，socket 为 `root:服务组 0660`。
- 即使其他账户属于服务组，Helper 的 peer credential 校验仍会拒绝其 RPC。
- systemd 对两个服务启用 `NoNewPrivileges`、`ProtectSystem`、`ProtectHome` 等限制，并仅开放必要写路径。
- Web 只在系统存在 `systemd-journal` 组时通过补充组读取日志。

## HTTP 安全

- 除登录外的 API 均要求认证。
- 密码使用 Argon2id，服务端只保存哈希。
- 会话 token 以哈希形式存储，Cookie 为 HttpOnly、SameSite=Strict。
- 修改请求校验 CSRF、Origin 和 Host。
- Origin 必须精确配置，不接受通配符。
- 登录失败有速率限制，会话有固定过期时间。

## systemd 与启动关系

- `avahi-manager-helper.socket` 在 sockets target 启用。
- Web 服务依赖 Helper socket，但 Helper 进程按需激活。
- 停止 Manager 会同时停止 Helper 和 socket，不影响 `avahi-daemon`。
- tmpfiles 在启动时重建 `/run/avahi-manager` 的所有权和权限。

## 明确不实现

当前范围不包括：

- 自研 mDNS、DNS Server、DHCP 或 Bonjour Gateway
- Docker、Kubernetes、集群或多服务器集中管理
- LDAP、OAuth、复杂 RBAC
- 插件系统、Prometheus、Grafana 或远程云管理
- 自定义 mDNS reflector

Avahi 自带的 `enable-reflector` 可以作为配置项管理，但 Manager 不实现自己的 reflector。

## 协议与格式依据

- [avahi-daemon.conf](https://github.com/avahi/avahi/blob/master/man/avahi-daemon.conf.5.xml.in)
- [静态服务 XML](https://github.com/avahi/avahi/blob/master/man/avahi.service.5.xml.in)
- [avahi.hosts](https://github.com/avahi/avahi/blob/master/man/avahi.hosts.5.xml.in)
- 目标系统安装的 `/usr/share/dbus-1/interfaces/org.freedesktop.Avahi.*.xml`
