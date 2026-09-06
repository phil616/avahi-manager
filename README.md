# Avahi Manager

Avahi Manager 是面向 Linux 的本地 Web 管理界面，用于查看和管理 Avahi、mDNS/DNS-SD 服务、主机映射、网络接口、日志与配置快照。

项目使用内嵌 React 前端的单个 Go 二进制。Web 服务以非 root 账户运行，需要特权的配置写入和 systemd 操作通过独立 root Helper 完成。

> 当前为开发版本。建议先在可恢复的 Debian/Ubuntu 测试机上验证，不要直接用于生产网络。

## 主要功能

- Avahi 状态、网络接口和实时服务发现
- `avahi-daemon.conf`、`hosts` 和静态 `.service` 管理
- Start、Stop、Restart、Reload、Enable、Disable
- 配置版本冲突检测、快照、失败回滚与审计日志
- 单管理员认证、CSRF/Origin/Host 校验和登录限流
- 非 root Web、root Helper 和 Unix socket 权限隔离

## 快速安装

目标机需要 systemd、D-Bus、`avahi-daemon` 和 `avahi-daemon.socket`。

从 GitHub Release 下载 Linux AMD64 压缩包后：

```bash
tar -xzf avahi-manager-<version>-linux-amd64.tar.gz
cd avahi-manager-<version>-linux-amd64
./install.sh
```

安装脚本会自动调用 `sudo`、创建服务账户和目录、初始化网页登录管理员、安装 systemd 单元并启动服务。默认访问地址为 `http://127.0.0.1:8053`。

常用命令：

```bash
./install.sh status
./install.sh start
./install.sh stop
./install.sh restart
./install.sh logs
./install.sh uninstall
./install.sh uninstall --purge
```

完整选项、更新方式、远程访问和故障排查见[使用指南](docs/USAGE.md)。

## 从源码构建

构建环境需要 Linux、Go 1.26+、Node.js 24、npm 和 GNU Make：

```bash
make check
make dist
```

`make dist` 生成 `dist/avahi-manager` 和 `dist/install.sh`。运行时不需要 Node.js 或独立 Web 服务器。

## 发布

推送 `v*` 标签会自动创建只包含 Linux AMD64 构建的 GitHub Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```

具体规则见[发布指南](docs/RELEASE.md)。

## 文档

- [使用、安装与排障](docs/USAGE.md)
- [架构与安全边界](docs/ARCHITECTURE.md)
- [开发与验收状态](DEVELOPMENT.md)
- [标签与 Release](docs/RELEASE.md)
