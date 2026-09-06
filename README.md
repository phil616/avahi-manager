# Avahi Manager

根据 [PLAN.md](PLAN.md) 开发的 Linux 原生 Avahi 管理平台。已有内嵌 React 八页界面、Go REST API、认证、配置/服务/主机管理、实时发现、日志和特权 Helper。项目仍在实现中，APT 更新、完整目录备份和生产打包尚未完成。

Avahi 继续负责 mDNS/DNS-SD。`/etc/avahi` 是配置事实来源，SQLite 仅保存管理员、会话、审计、任务、快照索引与管理状态。最终交付为内嵌 React 的单个 Go 程序，分别运行非 root `serve` 和 root `helper` 模式。

## 当前可运行内容

完整启动流程和注意事项见 **[使用指南](docs/USAGE.md)**，包括首次登录、Helper 启动、账户与目录权限、备份恢复和 socket 报错排查。

前端默认使用简体中文，八个页面均提供中文操作提示；配置键名、协议标识、原始日志和错误诊断保留原文，便于排查。页面内补充了保存重启、接口白名单、反射器风险及快照范围等说明。更新前端后需运行 `make build` 重新生成内嵌资源的二进制，再重启 Web 进程并刷新浏览器；已有账户无需重新初始化。

构建需要 Linux、Go 1.26+、Node.js 24 和 npm；运行生成的二进制不需要 Node.js。隔离 D-Bus 集成测试需要 `dbus-daemon`。以下诊断命令不修改宿主机配置。

```bash
make check
./bin/avahi-manager detect
./bin/avahi-manager interfaces
./bin/avahi-manager discover --duration 10s
```

真实浏览器测试使用临时 Avahi 配置、SQLite、Unix Helper 和模拟系统控制器，不修改宿主机：

```bash
cd frontend
npx playwright install chromium
cd ..
make e2e
```

仅预览 Web 界面（普通用户执行，不启动 Helper）：

```bash
mkdir -p .dev
./bin/avahi-manager init-admin --database "$PWD/.dev/manager.db"
./bin/avahi-manager serve --database "$PWD/.dev/manager.db"
```

打开 `http://127.0.0.1:8053`，默认用户名 `admin`，密码为初始化时输入的密码，无默认密码。已有管理员时跳过初始化。未启动 Helper 时，配置和快照等页面出现 `helper.sock: no such file or directory` 表示依赖缺失，不是完整可用状态。连接真实 Avahi 请按[使用指南](docs/USAGE.md)启动两个进程。

默认监听 `127.0.0.1:8053`，除登录入口外的 API 都要求登录。`serve --origins` 设置允许的准确 Origin（包含协议和端口）；修改访问地址时需同步配置 Origin。账户、日志权限和安全目录的自动部署仍待打包阶段交付，当前不是生产就绪版本。

`detect` 检查 OS、二进制、systemd service/socket、Avahi D-Bus 和原生配置，输出 JSON。D-Bus 查询使用 NoAutoStart，查看停止的服务不会触发启动。`interfaces` 使用 Netlink；`discover` 使用 Avahi D-Bus 信号与 ResolveService，在指定时长后输出发现快照（添加 `--events` 可输出全部缓存变化），不调用 `avahi-browse`。

## 已有模块

- `internal/config`：INI / hosts / 多服务 XML 模型及校验；SHA256 版本冲突检测；临时文件 fsync、原子 rename、快照、健康检查失败回滚、崩溃恢复记录、保留 20 个快照；fsnotify 目录监听。
- `internal/systemd`：严格限定 Avahi service/socket 的 Start / Stop / Restart / Reload / Enable / Disable，等待 systemd job 完成。
- `internal/avahi`：状态、服务解析、主机解析、实时发现、断线重连、内存缓存与订阅。
- `internal/network` / `internal/detect`：Netlink 接口与地址读取、安装及运行状态分类。
- `internal/database`：SQLite WAL、迁移、设置、管理文件哈希、漂移记录、审计和任务。
- `internal/auth` / `internal/api`：单管理员、Argon2id、哈希会话、12 小时过期、注销、登录限流、HttpOnly / SameSite=Strict Cookie、HTTPS Secure Cookie、Origin/Host 校验、CSRF、修改前后持久审计及 SSE 会话复查。
- `internal/helper`：Unix socket RPC、双向 SO_PEERCRED 校验、严格业务请求、配置/服务/快照/生命周期操作，以及启动时恢复未完成事务。同一程序已加入 root `helper` 入口，支持 systemd socket activation；部署目录与账户仍需后续打包阶段配置。
- `internal/journal`：固定 journalctl argv、级别/字面搜索/时间范围、受限读取和实时 Follow。
- `frontend`：React + TypeScript + Vite；Dashboard、Discovery、Services、Hosts、Interfaces、Settings、Logs、Maintenance；结构化表单、实时事件、备份与审计，桌面与移动布局。构建资源通过 go:embed 打包。

最近接受的配置有独立的受保护 baseline 快照。普通备份不会改变 baseline；Manager 重启后外部修改仍被标记为漂移。保存成功、显式 Reload from disk 或 Restore 才会更新接受的版本。恢复中断事务时同时恢复文件和 baseline 指针。

测试只在临时目录和独立 D-Bus 总线上执行修改与故障注入；不对本机 Avahi 执行写入、重启或 APT 操作。

## 尚未交付

完整剩余工作、已验证证据和验收清单见 [DEVELOPMENT.md](DEVELOPMENT.md)。APT 安装/更新、完整 `/etc/avahi/*` 辅助文件归档、manager.toml、systemd/Debian 包及完整生产权限验证仍待实现；当前不是已完成生产验收的发布版本。

## 依据

配置格式以 Avahi 的官方说明为依据：[daemon 配置](https://github.com/avahi/avahi/blob/master/man/avahi-daemon.conf.5.xml.in)、[静态服务 XML](https://github.com/avahi/avahi/blob/master/man/avahi.service.5.xml.in)、[hosts](https://github.com/avahi/avahi/blob/master/man/avahi.hosts.5.xml.in)。D-Bus 参数签名对照宿主机包提供的 `/usr/share/dbus-1/interfaces/org.freedesktop.Avahi.*.xml`，并通过私有 D-Bus 集成测试验证。
