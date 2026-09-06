# 发布指南

## 发布目标

GitHub Release 只提供 Linux AMD64 构建。每个版本包含：

```text
avahi-manager-<version>-linux-amd64.tar.gz
avahi-manager-<version>-linux-amd64.tar.gz.sha256
```

压缩包中的 `avahi-manager` 与 `install.sh` 位于同一目录，可以直接按 [USAGE.md](USAGE.md) 安装。不发布 Windows、macOS、ARM 或容器镜像。

GitHub 自动显示的 Source code 压缩包是平台功能，不属于项目构建产物。

## 创建版本

工作流由 `.github/workflows/release.yml` 定义，只响应 `v*` 标签：

```bash
git tag v0.1.0
git push origin v0.1.0
```

建议使用语义化版本：

- 正式版：`v1.2.3`
- 预发布版：`v1.2.3-rc.1`

标签中包含连字符时，工作流会把 Release 标记为 prerelease。

## 工作流步骤

1. 在 Ubuntu 24.04 AMD64 runner 检出标签对应提交。
2. 根据 `go.mod` 安装 Go，并安装 Node.js 24。
3. 执行安装脚本语法检查和 `make check`。
4. 使用 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 构建发布二进制。
5. 将二进制和安装脚本打包为 `tar.gz`。
6. 生成 SHA-256 校验文件。
7. 使用标签自动生成 Release Notes 并上传两个文件。

工作流只授予 Release job `contents: write`，checkout 不持久化 Git 凭据。重新运行同一标签的工作流会覆盖已有构建附件。

## 发布前检查

在 Linux 开发环境执行：

```bash
make check
make e2e
bash -n packaging/install.sh
git status --short
```

确认：

- 标签指向预期提交。
- 工作区没有遗漏的源文件或文档修改。
- `go.mod` 的 Go 版本可由 GitHub Actions 安装。
- `frontend/package-lock.json` 已与依赖同步。
- 安装脚本与二进制仍保持两文件分发约定。

## 下载验证

发布完成后，在 Linux AMD64 测试机下载两个附件并执行：

```bash
sha256sum -c avahi-manager-<version>-linux-amd64.tar.gz.sha256
tar -tzf avahi-manager-<version>-linux-amd64.tar.gz
```

正式发布验收还应覆盖首次安装、管理员初始化、系统重启、原地升级、普通卸载和 `--purge`。
