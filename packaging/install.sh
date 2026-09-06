#!/usr/bin/env bash
#
# Avahi Manager 一键安装脚本
#
# 在全新 Linux systemd 机器上：下载/使用单个 Go 二进制 -> 创建账户与目录 ->
# 生成 systemd 单元 -> enable --now 启动 -> 引导管理员密码。
#
# 用法：
#   sudo ./packaging/install.sh                # 使用 /etc/avahi-manager/manager.toml
#   sudo ./packaging/install.sh -c 路径.toml   # 指定配置
#   sudo ./packaging/install.sh -b ./bin/avahi-manager   # 指定二进制（跳过下载）
#   ./packaging/install.sh -u                  # 卸载（需 sudo）
#
# 配置事实来源：manager.toml（默认 packaging/manager.toml，安装后复制到
# /etc/avahi-manager/manager.toml）。systemd 单元由本脚本根据该文件生成。
# 注意：当前二进制仍以命令行参数运行，尚不原生解析 manager.toml。

set -euo pipefail

# ---- 解析 argparse ---------------------------------------------------------
CONFIG_FILE=""
BIN_PATH_ARG=""
UNINSTALL=0
usage() {
    sed -n '2,14p' "$0"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -c|--config) CONFIG_FILE="$2"; shift 2 ;;
        -b|--binary) BIN_PATH_ARG="$2"; shift 2 ;;
        -u|--uninstall) UNINSTALL=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "未知参数: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- 统一常量的默认值 -------------------------------------------------------
DEFAULT_BINARY="/usr/local/libexec/avahi-manager"
SYSTEMD_DIR="/etc/systemd/system"
TARGET_NAME="avahi-manager"
HELPER_NAME="avahi-manager-helper"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGING_DIR="$(cd "$SCRIPT_DIR" && pwd)"
UNIT_SRC="$PACKAGING_DIR/systemd"

# 简单的 TOML 标量/字符串数组解析器（无需 TOML 库）。
# 支持: key = "value"  以及  key = ["a", "b"]  -> 输出 a,b
toml_get() {
    local file="$1" key="$2"
    local line
    line="$(sed -nE "s/^[[:space:]]*${key}[[:space:]]*=[[:space:]]*(.*)[[:space:]]*$/\1/p" "$file" | head -n1)"
    [[ -z "$line" ]] && return 0
    # 去掉首尾方括号（数组形式）
    line="${line#\[}"; line="${line%\]}"
    # 移除所有双引号和空白
    line="$(printf '%s' "$line" | tr -d '"' | tr -d ' ')"
    printf '%s' "$line"
}

ensure_config() {
    if [[ -n "$CONFIG_FILE" ]]; then
        if [[ ! -f "$CONFIG_FILE" ]]; then
            echo "错误: 不存在配置 $CONFIG_FILE" >&2
            exit 1
        fi
        return
    fi
    if [[ -f "/etc/avahi-manager/manager.toml" ]]; then
        CONFIG_FILE="/etc/avahi-manager/manager.toml"
    elif [[ -f "$PACKAGING_DIR/manager.toml" ]]; then
        CONFIG_FILE="$PACKAGING_DIR/manager.toml"
    else
        echo "错误: 找不到 manager.toml (传入 -c 或放入 /etc/avahi-manager/)" >&2
        exit 1
    fi
}

if [[ "$UNINSTALL" -eq 0 ]]; then
    ensure_config

    USER="$(toml_get "$CONFIG_FILE" user)";               USER="${USER:-avahi-manager}"
    BINARY_PATH="$(toml_get "$CONFIG_FILE" binary_path)"; BINARY_PATH="${BINARY_PATH:-$DEFAULT_BINARY}"
    AVAHI_CONFIG_DIR="$(toml_get "$CONFIG_FILE" avahi_config_dir)"; AVAHI_CONFIG_DIR="${AVAHI_CONFIG_DIR:-/etc/avahi}"
    DATABASE_PATH="$(toml_get "$CONFIG_FILE" database_path)"; DATABASE_PATH="${DATABASE_PATH:-/var/lib/avahi-manager/manager.db}"
    DEFAULT_BACKUPS="/var/lib/avahi-manager-helper/backups"
    BACKUPS_DIR="$(toml_get "$CONFIG_FILE" backups_dir)";  BACKUPS_DIR="${BACKUPS_DIR:-$DEFAULT_BACKUPS}"
    DEFAULT_SOCKET="/run/avahi-manager/helper.sock"
    SOCKET_PATH="$(toml_get "$CONFIG_FILE" socket_path)";  SOCKET_PATH="${SOCKET_PATH:-$DEFAULT_SOCKET}"
    LISTEN="$(toml_get "$CONFIG_FILE" listen)";            LISTEN="${LISTEN:-127.0.0.1:8053}"
    ORIGINS_JSON="$(toml_get "$CONFIG_FILE" origins)";     ORIGINS="${ORIGINS_JSON:-http://127.0.0.1:8053}"

    DB_DIR="$(dirname "$DATABASE_PATH")"
else
    # 卸载模式不要求配置存在。
    true
fi

# ---- 权限检查（安装模式） ---------------------------------------------------
if [[ "$UNINSTALL" -eq 0 && "$(id -u)" -ne 0 ]]; then
    echo "安装需要 root 权限，请以 sudo 运行。" >&2
    exit 1
fi

# ---- 打印将要做的事 ---------------------------------------------------------
announce() {
    echo
    echo "=============================================="
    echo " Avahi Manager 安装"
    echo "=============================================="
    echo "  服务账户       : $USER"
    echo "  二进制          : $BINARY_PATH"
    echo "  Avahi 配置目录 : $AVAHI_CONFIG_DIR"
    echo "  数据库          : $DATABASE_PATH"
    echo "  备份目录        : $BACKUPS_DIR"
    echo "  socket          : $SOCKET_PATH"
    echo "  HTTP            : $LISTEN  (Origin: $ORIGINS)"
    echo "=============================================="
}

# ---- 安装逻辑 ---------------------------------------------------------------
do_install() {
    announce

    # 1) 校验 Avahi 已就绪（配置事实来源必须存在）。
    if [[ ! -d "$AVAHI_CONFIG_DIR" ]]; then
        echo "错误: 配置目录 $AVAHI_CONFIG_DIR 不存在。" >&2
        echo "请先安装 Avahi: sudo apt install avahi-daemon" >&2
        exit 1
    fi

    # 2) 决定二进制来源。
    local bin_src
    if [[ -n "$BIN_PATH_ARG" ]]; then
        bin_src="$BIN_PATH_ARG"
    elif [[ -f "$PACKAGING_DIR/../bin/avahi-manager" ]]; then
        bin_src="$PACKAGING_DIR/../bin/avahi-manager"
    else
        echo "错误: 未找到二进制。请用 -b 指定，或先执行 make check。" >&2
        exit 1
    fi
    if [[ ! -x "$bin_src" ]]; then
        echo "错误: $bin_src 不可执行。" >&2
        exit 1
    fi

    # 3) 不覆盖已有部署（除非显式卸载）。
    if getent passwd "$USER" >/dev/null 2>&1 && [[ -e "$BINARY_PATH" ]]; then
        echo "检测到已存在部署（账户 $USER 和 $BINARY_PATH）。" >&2
        echo "如需重新安装，请先执行: sudo $0 -u" >&2
        exit 1
    fi

    # 4) 创建系统账户（仅当不存在）。
    if ! getent passwd "$USER" >/dev/null 2>&1; then
        useradd --system --user-group --no-create-home \
            --home-dir "$DB_DIR" \
            --shell /usr/sbin/nologin "$USER"
        echo "  已创建系统账户: $USER"
    else
        echo "  账户 $USER 已存在，跳过创建。"
    fi

    # 5) 安装二进制。
    install -d -o root -g root -m 0755 "$(dirname "$BINARY_PATH")"
    install -o root -g root -m 0755 "$bin_src" "$BINARY_PATH"
    echo "  已安装二进制: $BINARY_PATH"

    # 6) 创建数据目录。
    install -d -o "$USER" -g "$USER" -m 0700 "$DB_DIR"
    install -d -o root  -g root  -m 0700 "$(dirname "$BACKUPS_DIR")"
    install -d -o root  -g root  -m 0700 "$BACKUPS_DIR"
    mkdir -p /run/avahi-manager && chown root:"$USER" /run/avahi-manager && chmod 0750 /run/avahi-manager
    echo "  已创建数据目录，权限符合非 root Web + root Helper 隔离要求。"

    # 7) 保留配置副本。
    install -d -o root -g root -m 0755 /etc/avahi-manager
    install -o root -g root -m 0644 "$CONFIG_FILE" /etc/avahi-manager/manager.toml

    # 8) 渲染 systemd 单元。
    render()
    {
        sed -e "s|@@USER@@|$USER|g" \
            -e "s|@@BINARY_PATH@@|$BINARY_PATH|g" \
            -e "s|@@AVAHI_CONFIG_DIR@@|$AVAHI_CONFIG_DIR|g" \
            -e "s|@@DATABASE_PATH@@|$DATABASE_PATH|g" \
            -e "s|@@DATABASE_DIR@@|$DB_DIR|g" \
            -e "s|@@BACKUPS_DIR@@|$BACKUPS_DIR|g" \
            -e "s|@@SOCKET_PATH@@|$SOCKET_PATH|g" \
            -e "s|@@LISTEN@@|$LISTEN|g" \
            -e "s|@@ORIGINS@@|$ORIGINS|g" \
            "$1"
    }

    install -o root -g root -m 0644 <(render "$UNIT_SRC/$TARGET_NAME.service") "$SYSTEMD_DIR/$TARGET_NAME.service"
    install -o root -g root -m 0644 <(render "$UNIT_SRC/$HELPER_NAME.service") "$SYSTEMD_DIR/$HELPER_NAME.service"
    install -o root -g root -m 0644 <(render "$UNIT_SRC/$HELPER_NAME.socket") "$SYSTEMD_DIR/$HELPER_NAME.socket"
    echo "  已生成 systemd 单元: $TARGET_NAME{, -helper.service, -helper.socket}"

    systemctl daemon-reload

    # 9) 首次运行需要引导管理员密码（无默认密码）。
    runuser -u "$USER" -- "$BINARY_PATH" init-admin --database "$DATABASE_PATH" \
        || { echo "注意: 管理员可能已存在；若未设置请手动运行 init-admin。" >&2; }

    # 10) 启用并启动（helper 由 socket 按需拉起）。
    systemctl enable --now "$HELPER_NAME.socket"
    systemctl enable --now "$TARGET_NAME.service"

    echo
    echo "已启动。请访问: http://${LISTEN}"
    echo
    echo "  查看状态: systemctl status $TARGET_NAME $HELPER_NAME.socket"
    echo "  查看日志: journalctl -u $TARGET_NAME -f"
    echo
}

do_uninstall() {
    if [[ "$(id -u)" -ne 0 ]]; then
        echo "卸载需要 root 权限。" >&2
        exit 1
    fi
    echo "正在停止并移除 Avahi Manager 服务..."
    systemctl disable --now "$TARGET_NAME.service" 2>/dev/null || true
    systemctl disable --now "$HELPER_NAME.socket"   2>/dev/null || true
    systemctl disable --now "$HELPER_NAME.service"  2>/dev/null || true
    rm -f "$SYSTEMD_DIR/$TARGET_NAME.service" \
          "$SYSTEMD_DIR/$HELPER_NAME.service" \
          "$SYSTEMD_DIR/$HELPER_NAME.socket"
    systemctl daemon-reload
    echo
    echo "systemd 单元已移除。数据未删除，如需清除请手动删除："
    echo "  - 数据库:     /var/lib/avahi-manager/"
    echo "  - 备份目录:   /var/lib/avahi-manager-helper/"
    echo "  - 账户:       userdel avahi-manager"
    echo
}

if [[ "$UNINSTALL" -eq 1 ]]; then
    do_uninstall
else
    do_install
fi
