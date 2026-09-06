# Avahi Manager 使用指南

本文档说明 Linux systemd 主机上的安装、更新、卸载、访问和故障排查。架构及权限设计见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 1. 环境要求

### 运行环境

- Linux，使用 systemd 作为 init
- 系统 D-Bus
- `avahi-daemon`、`avahi-daemon.service` 和 `avahi-daemon.socket`
- `/etc/avahi` 配置目录
- `/usr/bin/journalctl`
- Bash、sudo、shadow-utils/passwd 和 util-linux 提供的常用系统工具

安装器不会调用 apt、dnf 等包管理器。请先根据发行版安装并配置 Avahi。

### 构建环境

从源码构建还需要：

- Go 1.26+
- Node.js 24 和 npm
- GNU Make
- race test 所需的 C 工具链
- E2E 测试所需的 Playwright Chromium

## 2. 从 Release 安装

Release 当前只提供 Linux AMD64。下载压缩包和相应的 `.sha256` 文件后先校验：

```bash
sha256sum -c avahi-manager-<version>-linux-amd64.tar.gz.sha256
tar -xzf avahi-manager-<version>-linux-amd64.tar.gz
cd avahi-manager-<version>-linux-amd64
```

目录中必须同时存在：

```text
avahi-manager
install.sh
```

执行安装：

```bash
./install.sh
```

脚本会在需要时自动调用 sudo，并完成：

1. 检查 systemd、Avahi、journalctl 和二进制。
2. 创建不可登录的 `avahi-manager` 系统账户。
3. 安装二进制、配置、systemd 单元和 tmpfiles 规则。
4. 设置数据库、备份目录和 Unix socket 权限。
5. 以服务账户交互式初始化网页登录管理员。
6. 启用并启动 socket 与 Web 服务。

首次初始化没有默认密码。密码至少需要 12 字节，且不会写入 shell 参数或配置文件。

## 3. 安装选项

查看完整帮助：

```bash
./install.sh --help
```

常用定制示例：

```bash
./install.sh \
  --user avahi-manager \
  --admin-user admin \
  --listen 127.0.0.1:8053 \
  --origins http://127.0.0.1:8053,http://localhost:8053 \
  --config-dir /etc/avahi
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--user` | `avahi-manager` | 非 root Web 服务账户 |
| `--admin-user` | `admin` | 首次创建的网页登录用户名 |
| `--listen` | `127.0.0.1:8053` | HTTP 监听地址 |
| `--origins` | 本地 8053 的两个 Origin | 允许的精确浏览器 Origin |
| `--config-dir` | `/etc/avahi` | Avahi 原生配置目录 |

如果已经安装，重复运行同一版本或新版本的 `install.sh` 会原地更新二进制和 systemd 配置，同时保留管理员、数据库和备份。已安装后不能直接改变服务账户；如确需改变，先备份数据并执行彻底卸载。

## 4. 服务管理

```bash
./install.sh status
./install.sh start
./install.sh stop
./install.sh restart
./install.sh logs
```

也可以直接使用 systemd：

```bash
sudo systemctl status avahi-manager.service avahi-manager-helper.socket
sudo journalctl -u avahi-manager.service -u avahi-manager-helper.service -f
```

Helper 使用 socket activation，通常不需要单独启动。

## 5. 卸载

普通卸载会移除服务、systemd 单元、tmpfiles 规则、运行时 socket 和已安装二进制，但保留数据库、备份及服务账户：

```bash
./install.sh uninstall
```

彻底卸载会额外删除数据库、备份、安装状态，以及由脚本创建的服务账户：

```bash
./install.sh uninstall --purge
```

`--purge` 不会删除 `/etc/avahi`，也不会停止或卸载 Avahi。此操作不可恢复，应先自行备份重要数据。

## 6. 安装路径与权限

| 路径 | 所有者/权限 | 用途 |
| --- | --- | --- |
| `/usr/local/libexec/avahi-manager` | `root:root` / `0755` | 应用二进制 |
| `/etc/avahi-manager/manager.toml` | `root:root` / `0644` | 实际部署参数记录 |
| `/var/lib/avahi-manager` | 服务账户 / `0700` | SQLite 数据库与会话 |
| `/var/lib/avahi-manager-helper` | `root:root` / `0700` | 快照、baseline 和安装状态 |
| `/run/avahi-manager` | `root:服务组` / `0750` | Helper socket 目录 |
| `/run/avahi-manager/helper.sock` | `root:服务组` / `0660` | Web 与 Helper 通信 |

Web 账户不能写 `/etc/avahi` 或 root Helper 数据。Helper 还会校验客户端的实际 UID，因此同组其他账户不能调用特权 RPC。

## 7. 登录与访问

默认打开：

```text
http://127.0.0.1:8053
```

使用安装时创建的管理员登录。应用只支持一个管理员账户，没有默认密码。

远程访问建议保留回环监听并使用 SSH 隧道：

```bash
ssh -N -L 8053:127.0.0.1:8053 user@server
```

然后在本机打开 `http://127.0.0.1:8053`。

使用 HTTPS 反向代理时，应同时设置准确的外部 Origin：

```bash
./install.sh \
  --listen 127.0.0.1:8053 \
  --origins https://avahi.example.com
```

反向代理必须保留 Host，并支持 SSE 长连接。不要把未加密的管理端口直接暴露到公网。

## 8. 页面与配置行为

- **Dashboard**：Avahi、D-Bus、接口、服务数量和配置漂移状态。
- **Discovery**：浏览和解析网络中的 mDNS/DNS-SD 服务。
- **Services**：管理 `/etc/avahi/services/*.service`。
- **Hosts**：管理 `/etc/avahi/hosts`。
- **Interfaces**：配置允许或拒绝的网络接口。
- **Settings**：管理主机名、域、IPv4/IPv6、发布和 reflector 选项。
- **Logs**：查询并跟随 `avahi-daemon.service` 的 journald 日志。
- **Maintenance**：Avahi 生命周期、配置快照、恢复和审计。

`/etc/avahi` 始终是配置事实来源。页面读取配置时会获得 revision；保存时若磁盘已被其他工具修改，服务返回冲突而不是覆盖外部变更。

不同配置触发的动作不同：

- 静态 `.service` 文件通常执行 Reload。
- daemon 和 hosts 修改执行 Restart。
- 每次写入前创建快照，健康检查失败会尝试回滚。
- Reload from disk 表示接受当前磁盘内容为新的管理 baseline。

当前快照覆盖受管理的 `avahi-daemon.conf`、`hosts` 和 `services/*.service`，不是整个 `/etc/avahi` 的完整归档。

## 9. 诊断命令

以下命令只读，不修改 Avahi：

```bash
./avahi-manager detect
./avahi-manager interfaces
./avahi-manager discover --duration 10s
./avahi-manager discover --duration 30s --events
```

安装后的二进制路径是 `/usr/local/libexec/avahi-manager`。

## 10. 常见问题

| 现象 | 检查方式 |
| --- | --- |
| 页面无法访问 | `./install.sh status`，检查 8053 端口和服务日志 |
| `helper.sock` 不存在 | 检查 `avahi-manager-helper.socket` 和 tmpfiles 运行目录 |
| socket `permission denied` | 检查 Web 实际 UID、目录组和 socket 的 `0660` 权限 |
| `administrator is not initialized` | 确认安装初始化成功，且 init-admin 与 serve 使用同一数据库 |
| `administrator already exists` | 数据库已初始化，直接登录；安装器会自动跳过 |
| 403 Origin/Host | 访问协议、主机、端口必须与 `--origins` 精确匹配 |
| 401 或会话过期 | 重新登录，确认代理协议与 Secure Cookie 一致 |
| 429 | 登录限流生效，等待后核对用户名和密码 |
| 配置 revision 冲突 | 重新读取磁盘配置，再合并或接受外部修改 |
| Logs 无内容或权限错误 | 检查服务账户是否获得 `systemd-journal` 补充组 |
| Avahi 操作失败 | 检查 `avahi-daemon.service`、`.socket`、D-Bus 和原生配置语法 |

## 11. 从源码开发

构建并运行检查：

```bash
make check
```

生成与 Release 相同布局的目录：

```bash
make dist
ls -l dist/avahi-manager dist/install.sh
```

仅预览 Web 界面时，可以普通用户运行，不启动 root Helper：

```bash
mkdir -p .dev
./bin/avahi-manager init-admin --database "$PWD/.dev/manager.db"
./bin/avahi-manager serve --database "$PWD/.dev/manager.db"
```

该模式不能修改真实 Avahi 配置，也不能使用快照和生命周期操作。

浏览器端到端测试：

```bash
cd frontend
npm ci --no-audit --no-fund
npx playwright install chromium
cd ..
make e2e
```

开发与验收状态见 [../DEVELOPMENT.md](../DEVELOPMENT.md)。
