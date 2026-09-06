#!/usr/bin/env bash
# Install, inspect, or remove Avahi Manager on a systemd host.
# The distribution directory must contain this script and avahi-manager.

set -Eeuo pipefail
IFS=$'\n\t'
umask 022

PROGRAM="avahi-manager"
SERVICE="avahi-manager.service"
HELPER_SERVICE="avahi-manager-helper.service"
HELPER_SOCKET="avahi-manager-helper.socket"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
SCRIPT_PATH="$SCRIPT_DIR/$(basename -- "${BASH_SOURCE[0]}")"
SOURCE_BINARY="$SCRIPT_DIR/$PROGRAM"

BINARY_PATH="/usr/local/libexec/avahi-manager"
CONFIG_DIR="/etc/avahi-manager"
CONFIG_FILE="$CONFIG_DIR/manager.toml"
SYSTEMD_DIR="/etc/systemd/system"
TMPFILES_FILE="/etc/tmpfiles.d/avahi-manager.conf"
DATABASE_DIR="/var/lib/avahi-manager"
DATABASE_PATH="$DATABASE_DIR/manager.db"
BACKUP_ROOT="/var/lib/avahi-manager-helper"
BACKUPS_DIR="$BACKUP_ROOT/backups"
INSTALL_STATE="$BACKUP_ROOT/install-state"
RUNTIME_DIR="/run/avahi-manager"
SOCKET_PATH="$RUNTIME_DIR/helper.sock"

ACTION="install"
SERVICE_USER=""
SERVICE_USER_EXPLICIT=0
ADMIN_USER="admin"
LISTEN="127.0.0.1:8053"
ORIGINS="http://127.0.0.1:8053,http://localhost:8053"
AVAHI_CONFIG_DIR="/etc/avahi"
PURGE=0

say() { printf '%s\n' "$*"; }
die() { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() {
    cat <<'EOF'
Avahi Manager 安装与管理脚本

用法：
  ./install.sh [install] [选项]   安装或原地更新并启动服务（默认）
  ./install.sh status             查看安装及 systemd 状态
  ./install.sh start|stop|restart 管理 systemd 服务
  ./install.sh logs               持续查看 Web 与 Helper 日志
  ./install.sh uninstall          卸载程序，保留数据库和备份
  ./install.sh uninstall --purge  卸载并删除数据库、备份和脚本创建的账户

安装选项：
  --user NAME          服务账户（默认 avahi-manager）
  --admin-user NAME    首次初始化的网页登录用户名（默认 admin）
  --listen ADDRESS     HTTP 监听地址（默认 127.0.0.1:8053）
  --origins LIST       允许的 Origin，多个用逗号分隔
  --config-dir PATH    Avahi 配置目录（默认 /etc/avahi）
  -h, --help           显示帮助

install.sh 必须和名为 avahi-manager 的可执行文件放在同一目录。安装和卸载
会在需要时自动通过 sudo 获取 root 权限。首次安装会交互式初始化网页登录
管理员；密码不会出现在命令行或配置文件中。
EOF
}

need_value() {
    [[ $# -ge 2 && -n "$2" ]] || die "$1 缺少参数值"
}

ORIGINAL_ARGS=("$@")
while (($#)); do
    case "$1" in
        install|status|start|stop|restart|logs|uninstall) ACTION="$1"; shift ;;
        remove|-u|--uninstall) ACTION="uninstall"; shift ;;
        --purge) PURGE=1; shift ;;
        --user) need_value "$@"; SERVICE_USER="$2"; SERVICE_USER_EXPLICIT=1; shift 2 ;;
        --admin-user) need_value "$@"; ADMIN_USER="$2"; shift 2 ;;
        --listen) need_value "$@"; LISTEN="$2"; shift 2 ;;
        --origins) need_value "$@"; ORIGINS="$2"; shift 2 ;;
        --config-dir) need_value "$@"; AVAHI_CONFIG_DIR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "未知参数: $1（使用 --help 查看帮助）" ;;
    esac
done

[[ "$PURGE" -eq 0 || "$ACTION" == "uninstall" ]] || die "--purge 只能与 uninstall 一起使用"

state_get() {
    local key="$1"
    [[ -r "$INSTALL_STATE" ]] || return 0
    awk -F= -v wanted="$key" '$1 == wanted { sub(/^[^=]*=/, ""); print; exit }' "$INSTALL_STATE"
}

# Reuse the account selected by an earlier installation unless overridden.
INSTALLED_SERVICE_USER="$(state_get SERVICE_USER)"
if [[ -z "$SERVICE_USER" ]]; then
    SERVICE_USER="$INSTALLED_SERVICE_USER"
    SERVICE_USER="${SERVICE_USER:-avahi-manager}"
fi

validate_options() {
    local port origin origin_host origin_port
    local -a origin_items=()
    [[ "$SERVICE_USER" =~ ^[a-z_][a-z0-9_-]{0,30}$ ]] || die "服务账户名无效: $SERVICE_USER"
    [[ "$ADMIN_USER" =~ ^[A-Za-z0-9_.@-]{1,64}$ ]] || die "管理员用户名无效: $ADMIN_USER"
    [[ "$LISTEN" =~ ^(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9._-]+):([0-9]{1,5})$ ]] || die "监听地址格式无效: $LISTEN"
    port="${BASH_REMATCH[2]}"
    ((10#$port >= 1 && 10#$port <= 65535)) || die "监听端口超出范围: $port"
    [[ "$ORIGINS" != ,* && "$ORIGINS" != *, && "$ORIGINS" != *,,* ]] || die "Origin 列表中存在空项"
    local old_ifs="$IFS"
    IFS=',' read -r -a origin_items <<<"$ORIGINS"
    IFS="$old_ifs"
    ((${#origin_items[@]} > 0)) || die "Origin 列表不能为空"
    for origin in "${origin_items[@]}"; do
        [[ "$origin" =~ ^https?://(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9._-]+)(:([0-9]{1,5}))?$ ]] \
            || die "Origin 格式无效: $origin"
        origin_host="${BASH_REMATCH[1]}"
        origin_port="${BASH_REMATCH[3]:-}"
        [[ -n "$origin_host" ]] || die "Origin 主机不能为空"
        if [[ -n "$origin_port" ]]; then
            ((10#$origin_port >= 1 && 10#$origin_port <= 65535)) || die "Origin 端口超出范围: $origin_port"
        fi
    done
    [[ "$AVAHI_CONFIG_DIR" =~ ^/[A-Za-z0-9._/+:-]+$ && "$AVAHI_CONFIG_DIR" != *"/../"* && "$AVAHI_CONFIG_DIR" != *"/.." ]] \
        || die "Avahi 配置目录必须是安全的绝对路径"
}

elevate_if_needed() {
    [[ "$(id -u)" -eq 0 ]] && return 0
    command -v sudo >/dev/null 2>&1 || die "需要 root 权限，但系统中找不到 sudo；请以 root 运行本脚本"
    say "需要系统管理员权限，正在调用 sudo…"
    exec sudo -- "$SCRIPT_PATH" "${ORIGINAL_ARGS[@]}"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "缺少必需命令: $1"
}

assert_managed_dir() {
    [[ ! -L "$1" ]] || die "拒绝使用符号链接目录: $1"
}

assert_managed_file() {
    [[ ! -L "$1" ]] || die "拒绝覆盖符号链接文件: $1"
}

create_service_account() {
    local previous_created
    previous_created="$(state_get CREATED_USER)"
    if getent passwd "$SERVICE_USER" >/dev/null 2>&1; then
        [[ "$(id -u "$SERVICE_USER")" -ne 0 ]] || die "服务账户不能是 root"
        CREATED_USER="${previous_created:-0}"
        say "服务账户已存在: $SERVICE_USER"
        return
    fi

    local nologin_shell
    nologin_shell="$(command -v nologin || true)"
    [[ -n "$nologin_shell" ]] || nologin_shell="/usr/sbin/nologin"
    useradd --system --user-group --no-create-home \
        --home-dir "$DATABASE_DIR" --shell "$nologin_shell" "$SERVICE_USER"
    CREATED_USER=1
    say "已创建不可登录的系统账户: $SERVICE_USER"
}

render_files() {
    local service_group="$1" journal_line=""
    if getent group systemd-journal >/dev/null 2>&1; then
        journal_line="SupplementaryGroups=systemd-journal"
    fi

    cat >"$WORK_DIR/$SERVICE" <<EOF
[Unit]
Description=Avahi Manager web interface
After=network.target dbus.service $HELPER_SOCKET
Wants=$HELPER_SOCKET

[Service]
Type=exec
User=$SERVICE_USER
Group=$service_group
$journal_line
ExecStart=$BINARY_PATH serve --database $DATABASE_PATH --config-dir $AVAHI_CONFIG_DIR --helper $SOCKET_PATH --origins $ORIGINS --listen $LISTEN
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=$DATABASE_DIR $RUNTIME_DIR
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes

[Install]
WantedBy=multi-user.target
EOF

    cat >"$WORK_DIR/$HELPER_SERVICE" <<EOF
[Unit]
Description=Avahi Manager privileged helper
Requires=$HELPER_SOCKET
After=$HELPER_SOCKET dbus.service
PartOf=$SERVICE
Before=$SERVICE

[Service]
Type=exec
User=root
Group=root
ExecStart=$BINARY_PATH helper --manager-user $SERVICE_USER --config-dir $AVAHI_CONFIG_DIR --backups $BACKUPS_DIR --socket $SOCKET_PATH
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=$AVAHI_CONFIG_DIR $BACKUP_ROOT /etc/systemd/system $RUNTIME_DIR
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
EOF

    cat >"$WORK_DIR/$HELPER_SOCKET" <<EOF
[Unit]
Description=Avahi Manager privileged helper socket
PartOf=$SERVICE
Before=$SERVICE

[Socket]
ListenStream=$SOCKET_PATH
SocketUser=root
SocketGroup=$service_group
SocketMode=0660
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
EOF

    cat >"$WORK_DIR/manager.toml" <<EOF
# Generated by install.sh. Reinstall with the corresponding option to edit.
user = "$SERVICE_USER"
binary_path = "$BINARY_PATH"
avahi_config_dir = "$AVAHI_CONFIG_DIR"
database_path = "$DATABASE_PATH"
backups_dir = "$BACKUPS_DIR"
socket_path = "$SOCKET_PATH"
listen = "$LISTEN"
origins = ["${ORIGINS//,/\", \"}"]
EOF

    cat >"$WORK_DIR/tmpfiles.conf" <<EOF
d $RUNTIME_DIR 0750 root $service_group -
EOF
}

install_manager() {
    validate_options
    elevate_if_needed

    # state_get may have been unable to traverse the root-only state directory
    # before sudo. Re-read it after elevation.
    INSTALLED_SERVICE_USER="$(state_get SERVICE_USER)"
    if [[ "$SERVICE_USER_EXPLICIT" -eq 0 && -n "$INSTALLED_SERVICE_USER" ]]; then
        SERVICE_USER="$INSTALLED_SERVICE_USER"
    elif [[ -n "$INSTALLED_SERVICE_USER" && "$SERVICE_USER" != "$INSTALLED_SERVICE_USER" ]]; then
        die "已安装实例使用账户 $INSTALLED_SERVICE_USER；更换账户前请先使用 uninstall --purge"
    fi
    validate_options

    for command_name in awk getent id install mktemp runuser systemctl systemd-tmpfiles useradd; do
        require_command "$command_name"
    done
    [[ -d /run/systemd/system ]] || die "systemd 未作为当前系统的 init 运行"
    [[ -x /usr/sbin/avahi-daemon ]] || die "找不到 /usr/sbin/avahi-daemon；请先安装 avahi-daemon"
    [[ -x /usr/bin/journalctl ]] || die "找不到 /usr/bin/journalctl；日志功能需要完整的 systemd 工具"
    [[ -d "$AVAHI_CONFIG_DIR" ]] || die "找不到 $AVAHI_CONFIG_DIR；请先安装并配置 avahi-daemon"
    systemctl cat avahi-daemon.service avahi-daemon.socket >/dev/null 2>&1 \
        || die "找不到 avahi-daemon.service 或 avahi-daemon.socket"
    [[ -f "$SOURCE_BINARY" && -x "$SOURCE_BINARY" ]] \
        || die "请把可执行文件 avahi-manager 与 install.sh 放在同一目录"

    for managed_dir in "$CONFIG_DIR" "$DATABASE_DIR" "$BACKUP_ROOT" "$BACKUPS_DIR" "$RUNTIME_DIR"; do
        assert_managed_dir "$managed_dir"
    done
    for managed_file in "$BINARY_PATH" "$CONFIG_FILE" "$INSTALL_STATE" \
        "$SYSTEMD_DIR/$SERVICE" "$SYSTEMD_DIR/$HELPER_SERVICE" \
        "$SYSTEMD_DIR/$HELPER_SOCKET" "$TMPFILES_FILE"; do
        assert_managed_file "$managed_file"
    done

    create_service_account
    local service_group
    service_group="$(id -gn "$SERVICE_USER")"

    WORK_DIR="$(mktemp -d)"
    trap 'rm -rf -- "$WORK_DIR"' EXIT
    render_files "$service_group"

    # Stop old processes before replacing their executable. Missing units are
    # expected on a first installation.
    systemctl stop "$SERVICE" "$HELPER_SOCKET" "$HELPER_SERVICE" 2>/dev/null || true

    install -d -o root -g root -m 0755 "$(dirname "$BINARY_PATH")" "$CONFIG_DIR"
    if [[ ! "$SOURCE_BINARY" -ef "$BINARY_PATH" ]]; then
        install -o root -g root -m 0755 "$SOURCE_BINARY" "$BINARY_PATH"
    fi
    install -d -o "$SERVICE_USER" -g "$service_group" -m 0700 "$DATABASE_DIR"
    chown -R "$SERVICE_USER:$service_group" "$DATABASE_DIR"
    chmod 0700 "$DATABASE_DIR"
    install -d -o root -g root -m 0700 "$BACKUP_ROOT" "$BACKUPS_DIR"
    chown -R root:root "$BACKUP_ROOT"
    chmod 0700 "$BACKUP_ROOT" "$BACKUPS_DIR"

    install -o root -g root -m 0644 "$WORK_DIR/$SERVICE" "$SYSTEMD_DIR/$SERVICE"
    install -o root -g root -m 0644 "$WORK_DIR/$HELPER_SERVICE" "$SYSTEMD_DIR/$HELPER_SERVICE"
    install -o root -g root -m 0644 "$WORK_DIR/$HELPER_SOCKET" "$SYSTEMD_DIR/$HELPER_SOCKET"
    install -o root -g root -m 0644 "$WORK_DIR/tmpfiles.conf" "$TMPFILES_FILE"
    install -o root -g root -m 0644 "$WORK_DIR/manager.toml" "$CONFIG_FILE"
    cat >"$INSTALL_STATE" <<EOF
SERVICE_USER=$SERVICE_USER
SERVICE_GROUP=$service_group
CREATED_USER=$CREATED_USER
EOF
    chmod 0600 "$INSTALL_STATE"

    systemd-tmpfiles --create "$TMPFILES_FILE"
    if command -v systemd-analyze >/dev/null 2>&1; then
        systemd-analyze verify \
            "$SYSTEMD_DIR/$SERVICE" \
            "$SYSTEMD_DIR/$HELPER_SERVICE" \
            "$SYSTEMD_DIR/$HELPER_SOCKET" >/dev/null
    fi
    systemctl daemon-reload

    say "正在初始化网页登录管理员（已有管理员时会自动跳过）…"
    runuser -u "$SERVICE_USER" -- "$BINARY_PATH" init-admin \
        --if-missing --database "$DATABASE_PATH" --username "$ADMIN_USER"

    systemctl enable --now "$HELPER_SOCKET"
    systemctl enable --now "$SERVICE"

    say ""
    say "Avahi Manager 已安装并启动。"
    say "访问地址: http://$LISTEN"
    say "查看状态: $SCRIPT_PATH status"
    say "查看日志: sudo journalctl -u $SERVICE -f"
    say "卸载程序: $SCRIPT_PATH uninstall"
}

show_status() {
    say "二进制: $BINARY_PATH"
    if [[ -x "$BINARY_PATH" ]]; then say "安装状态: 已安装"; else say "安装状态: 未安装"; fi
    if command -v systemctl >/dev/null 2>&1; then
        systemctl --no-pager --full status "$SERVICE" "$HELPER_SOCKET" 2>/dev/null || true
    fi
}

manage_service() {
    elevate_if_needed
    require_command systemctl
    [[ -f "$SYSTEMD_DIR/$SERVICE" ]] || die "Avahi Manager 尚未安装"
    case "$ACTION" in
        start)
            require_command systemd-tmpfiles
            systemd-tmpfiles --create "$TMPFILES_FILE"
            systemctl start "$HELPER_SOCKET" "$SERVICE"
            ;;
        stop) systemctl stop "$SERVICE" ;;
        restart)
            require_command systemd-tmpfiles
            systemd-tmpfiles --create "$TMPFILES_FILE"
            systemctl restart "$HELPER_SOCKET" "$SERVICE"
            ;;
    esac
    systemctl --no-pager --full status "$SERVICE" "$HELPER_SOCKET" || true
}

show_logs() {
    elevate_if_needed
    require_command journalctl
    journalctl --unit "$SERVICE" --unit "$HELPER_SERVICE" --follow
}

uninstall_manager() {
    elevate_if_needed
    require_command systemctl

    local created_user installed_user
    created_user="$(state_get CREATED_USER)"
    installed_user="$(state_get SERVICE_USER)"
    installed_user="${installed_user:-$SERVICE_USER}"
    if [[ "$PURGE" -eq 1 && "$created_user" == "1" ]]; then
        require_command userdel
    fi

    systemctl disable --now "$SERVICE" "$HELPER_SOCKET" "$HELPER_SERVICE" 2>/dev/null || true
    rm -f -- \
        "$SYSTEMD_DIR/$SERVICE" \
        "$SYSTEMD_DIR/$HELPER_SERVICE" \
        "$SYSTEMD_DIR/$HELPER_SOCKET" \
        "$TMPFILES_FILE" \
        "$BINARY_PATH"
    systemctl daemon-reload
    systemctl reset-failed "$SERVICE" "$HELPER_SERVICE" 2>/dev/null || true
    rm -f -- "$SOCKET_PATH"
    rmdir -- "$RUNTIME_DIR" 2>/dev/null || true

    if [[ "$PURGE" -eq 1 ]]; then
        [[ "$DATABASE_DIR" == "/var/lib/avahi-manager" ]] || die "拒绝清理非预期数据库目录"
        [[ "$BACKUP_ROOT" == "/var/lib/avahi-manager-helper" ]] || die "拒绝清理非预期备份目录"
        if [[ "$created_user" == "1" ]] && getent passwd "$installed_user" >/dev/null 2>&1; then
            userdel "$installed_user"
            say "已删除脚本创建的系统账户: $installed_user"
        fi
        rm -rf -- "$DATABASE_DIR" "$BACKUP_ROOT" "$CONFIG_DIR"
        say "Avahi Manager 已彻底卸载；数据库和备份已删除。"
    else
        rm -f -- "$CONFIG_FILE"
        rmdir -- "$CONFIG_DIR" 2>/dev/null || true
        say "Avahi Manager 已卸载。数据库和备份仍保留："
        say "  $DATABASE_DIR"
        say "  $BACKUP_ROOT"
        if [[ "$created_user" == "1" ]]; then
            say "服务账户 $installed_user 已保留，以保持数据文件的所有权；使用 --purge 可一并删除。"
        fi
    fi
}

case "$ACTION" in
    install) install_manager ;;
    status) show_status ;;
    start|stop|restart) manage_service ;;
    logs) show_logs ;;
    uninstall) uninstall_manager ;;
esac
