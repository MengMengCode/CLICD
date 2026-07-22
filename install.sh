#!/bin/sh
set -eu

REPO="${CLICD_REPO:-MengMengCode/CLICD}"
CLICD_INSTALL_VERSION="${CLICD_VERSION:-latest}"
ACTION="${1:-install}"
ACTION_CONFIRM="${2:-}"
ISSUE_URL="https://github.com/${REPO}/issues"
LOG_FILE="${CLICD_LOG_FILE:-/var/log/clicd-install.log}"
INSTALL_DOWNLOAD_MARKER="${CLICD_INSTALL_DOWNLOAD_MARKER:-/tmp/clicd-install-dir.$$}"
LIBVIRT_DEFAULT_MARKER="/var/lib/clicd/kvm/default-network.created"

normalize_clicd_arch() {
    arch="$1"
    case "$(printf '%s' "$arch" | tr 'A-Z' 'a-z')" in
        x86_64|amd64) echo amd64 ;;
        aarch64|arm64) echo arm64 ;;
        *) echo "" ;;
    esac
}

HOST_ARCH_RAW="$(uname -m 2>/dev/null || echo unknown)"
CLICD_ARCH_NORMALIZED="$(normalize_clicd_arch "${CLICD_ARCH:-$HOST_ARCH_RAW}")"
ASSET_DIR="clicd-linux-${CLICD_ARCH_NORMALIZED:-unknown}"
ASSET="${ASSET_DIR}.tar.gz"
BINARY_ASSET="$ASSET_DIR"

kvm_supported_arch() {
    [ "$CLICD_ARCH_NORMALIZED" = "amd64" ] || [ "$CLICD_ARCH_NORMALIZED" = "arm64" ]
}

warn_kvm_unsupported_arch() {
    if ! kvm_supported_arch; then
        warn "当前架构 ${CLICD_ARCH_NORMALIZED:-unknown} 已适配 CLICD/LXC；KVM 功能当前支持 x86_64/amd64 和 aarch64/arm64，将跳过 KVM 专用依赖。"
    fi
}

qemu_system_package_apk() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo qemu-system-aarch64 ;;
        *) echo qemu-system-x86_64 ;;
    esac
}

qemu_system_package_apt() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo qemu-system-arm ;;
        *) echo qemu-system-x86 ;;
    esac
}

qemu_system_package_rpm() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo qemu-system-aarch64 ;;
        *) echo qemu-kvm ;;
    esac
}

qemu_emulator_cmd() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo qemu-system-aarch64 ;;
        *) echo qemu-system-x86_64 ;;
    esac
}

qemu_efi_package_apt() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo qemu-efi-aarch64 ;;
        *) echo ovmf ;;
    esac
}

qemu_efi_package_apk() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo edk2-aarch64 ;;
        *) echo ovmf ;;
    esac
}

qemu_efi_package_rpm() {
    case "$CLICD_ARCH_NORMALIZED" in
        arm64) echo edk2-aarch64 ;;
        *) echo edk2-ovmf ;;
    esac
}

normalize_lang() {
    lang="$1"
    case "$(printf '%s' "$lang" | tr 'A-Z' 'a-z')" in
        en|en_*|en-*) echo en ;;
        zh|zh_*|zh-*) echo zh ;;
        *) echo "" ;;
    esac
}

detect_lang() {
    normalized="$(normalize_lang "${CLICD_LANG:-}")"
    if [ -n "$normalized" ]; then
        echo "$normalized"
        return
    fi
    normalized="$(normalize_lang "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}")"
    if [ -n "$normalized" ]; then
        echo "$normalized"
        return
    fi
    echo zh
}

choose_language() {
    normalized="$(normalize_lang "${CLICD_LANG:-}")"
    if [ -n "$normalized" ]; then
        echo "$normalized"
        return
    fi
    case "$ACTION" in
        -h|--help|help)
            detect_lang
            return
            ;;
    esac
    if [ -t 0 ]; then
        {
            echo "====================================="
            echo "  请选择语言 / Select language"
            echo "====================================="
            echo "  1) 简体中文"
            echo "  2) English"
            printf "  请输入 1/2 [1]: "
        } >&2
        IFS= read -r answer || answer=""
        case "$answer" in
            2) echo en ;;
            *) echo zh ;;
        esac
        return
    fi
    if [ -r /dev/tty ] && [ -w /dev/tty ] && { printf '' > /dev/tty; } 2>/dev/null; then
        {
            echo "====================================="
            echo "  请选择语言 / Select language"
            echo "====================================="
            echo "  1) 简体中文"
            echo "  2) English"
            printf "  请输入 1/2 [1]: "
        } > /dev/tty
        IFS= read -r answer < /dev/tty || answer=""
        case "$answer" in
            2) echo en ;;
            *) echo zh ;;
        esac
        return
    fi
    detect_lang
}

CLICD_LANG_DETECTED="$(choose_language)"
export CLICD_LANG="$CLICD_LANG_DETECTED"

tr_msg() {
    msg="$*"
    [ "$CLICD_LANG_DETECTED" = "en" ] || { printf '%s' "$msg"; return; }
    msg="$(printf '%s' "$msg" | sed \
        -e 's/中文安装\/卸载脚本/Installer\/Uninstaller/g' \
        -e 's/警告/Warning/g' \
        -e 's/错误/Error/g' \
        -e 's/安装\/卸载未完成。请查看日志：/Install\/uninstall did not complete. Check log: /g' \
        -e 's/如果你确认这是程序问题，请提交 issue：/If this looks like a CLICD bug, please open an issue: /g' \
        -e 's/开始：/Starting: /g' \
        -e 's/完成：/Completed: /g' \
        -e 's/步骤失败：/Step failed: /g' \
        -e 's/退出码：/exit code: /g' \
        -e 's/最近 80 行日志：/Last 80 log lines: /g' \
        -e 's/请将上述日志和系统信息提交到：/Please submit the log above and system info to: /g' \
        -e 's/系统检测：/System check: /g' \
        -e 's/当前安装包仅支持/This installer only supports/g' \
        -e 's/当前架构：/current architecture: /g' \
        -e 's/未检测到 systemd 或 OpenRC，无法安装服务。/systemd or OpenRC was not detected; cannot install service./g' \
        -e 's/暂不支持当前 Linux 发行版：/Unsupported Linux distribution: /g' \
        -e 's/请提交 issue 并附上/Please open an issue with/g' \
        -e 's/发行版/Distribution/g' \
        -e 's/不在主要支持列表，将按检测到的软件包管理器尝试安装。/is not in the primary support list; trying the detected package manager./g' \
        -e 's/存储检测：/Storage check: /g' \
        -e 's/根文件系统=/root filesystem=/g' \
        -e 's/可用空间=/available space=/g' \
        -e 's/根分区可用空间低于 5GB，下载镜像或创建 KVM\/LXC 时可能失败。/Root partition has less than 5GB available; image downloads or KVM\/LXC creation may fail./g' \
        -e 's/请使用 root 权限运行：/Run as root: /g' \
        -e 's/或执行：/Or run: /g' \
        -e 's/卸载：/Uninstall: /g' \
        -e 's/问题反馈：/Issues: /g' \
        -e 's/日志文件：/Log file: /g' \
        -e 's/仓库地址：/Repository: /g' \
        -e 's/未知操作：/Unknown action: /g' \
        -e 's/卸载会停止并删除 CLICD 服务、配置数据库、CLICD 创建的 LXC\/KVM 实例和缓存数据。/Uninstall will stop and remove the CLICD service, configuration database, CLICD-created LXC\/KVM instances, and cached data./g' \
        -e 's/为避免误删生产数据，脚本只会删除名称形如 ct-数字 的 LXC 容器、clicd-img-dl-\* 下载临时容器和 vm-数字 的 KVM 域。/To avoid deleting production data, the script only removes LXC containers named ct-NUMBER, temporary clicd-img-dl-* download containers, and KVM domains named vm-NUMBER./g' \
        -e 's/如需确认卸载，请输入：YES/Type YES to confirm uninstall:/g' \
        -e 's/已取消卸载。如需非交互卸载，请设置 CLICD_UNINSTALL_CONFIRM=1。/Uninstall cancelled. For non-interactive uninstall, set CLICD_UNINSTALL_CONFIRM=1./g' \
        -e 's/正在卸载 CLICD.../Uninstalling CLICD.../g' \
        -e 's/正在删除 CLICD 创建的 LXC 容器（\/var\/lib\/lxc\/ct-数字）.../Removing CLICD-created LXC containers (\/var\/lib\/lxc\/ct-NUMBER).../g' \
        -e 's/保留 \/root\/clicd-backups，避免误删部署\/回滚备份。确认不需要后可手动删除。/Keeping \/root\/clicd-backups to avoid deleting deployment\/rollback backups. Remove it manually if no longer needed./g' \
        -e 's/CLICD 卸载完成/CLICD uninstall complete/g' \
        -e 's/已删除服务、二进制、SQLite\/配置数据、CLICD LXC\/KVM 实例、/Removed service, binary, SQLite\/config data, CLICD LXC\/KVM instances,/g' \
        -e 's/CLICD 镜像缓存、防火墙规则、主机钩子、配额记录和临时文件。/CLICD image cache, firewall rules, host hooks, quota records, and temporary files./g' \
        -e 's/已保留 \/root\/clicd-backups 和非 CLICD 的 LXC 全局缓存，避免误删生产备份\/共享镜像。/Kept \/root\/clicd-backups and non-CLICD global LXC cache to avoid deleting production backups\/shared images./g' \
        -e 's/日志：/Log: /g' \
        -e 's/兼容性检查/Compatibility check/g' \
        -e 's/存储环境检查/Storage environment check/g' \
        -e 's/安装系统依赖/Install system dependencies/g' \
        -e 's/配置内核网络参数/Configure kernel networking/g' \
        -e 's/配置运行时服务/Configure runtime services/g' \
        -e 's/配置 libvirt default NAT 网络/Configure libvirt default NAT network/g' \
        -e 's/配置 UID\/GID 映射/Configure UID\/GID mapping/g' \
        -e 's/配置 LXC 存储权限/Configure LXC storage permissions/g' \
        -e 's/检查 project quota/Check project quota/g' \
        -e 's/下载发行版包/Download release package/g' \
        -e 's/安装 CLICD 二进制/Install CLICD binary/g' \
        -e 's/安装并启动 CLICD 服务/Install and start CLICD service/g' \
        -e 's/已写入面板语言：/Panel language saved: /g' \
        -e 's/写入面板语言/Save panel language/g' \
        -e 's/面板语言写入失败，请安装后在面板右下角手动切换。/Failed to save panel language. Please switch it manually from the lower-left panel control after installation./g' \
        -e 's/正在使用 apk 安装依赖.../Installing dependencies with apk.../g' \
        -e 's/正在使用 apt 安装依赖.../Installing dependencies with apt.../g' \
        -e 's/正在使用 dnf 安装依赖.../Installing dependencies with dnf.../g' \
        -e 's/正在使用 yum 安装依赖.../Installing dependencies with yum.../g' \
        -e 's/依赖安装后仍未找到/Still missing after dependency installation: /g' \
        -e 's/，请检查 LXC 软件源\/安装日志。/. Check the LXC repository\/install log./g' \
        -e 's/，请检查系统网络工具包。/. Check the system network tools package./g' \
        -e 's/，请检查 iproute2 安装。/. Check the iproute2 installation./g' \
        -e 's/，请检查 libvirt-client\/libvirt-clients 安装。/. Check the libvirt-client\/libvirt-clients installation./g' \
        -e 's/，请检查 qemu-utils\/qemu-img 安装。/. Check the qemu-utils\/qemu-img installation./g' \
        -e 's/，请检查 cloud-image-utils\/cloud-utils 安装。/. Check the cloud-image-utils\/cloud-utils installation./g' \
        -e 's/可选依赖未安装：/Optional dependency was not installed: /g' \
        -e 's/当前系统 /Current system /g' \
        -e 's/ 未找到 dnf\/yum，无法安装依赖。/ does not have dnf\/yum; cannot install dependencies./g' \
        -e 's/Windows KVM 初始化需要 genisoimage、mkisofs 或 xorriso 中任意一个。/Windows KVM initialization requires one of genisoimage, mkisofs, or xorriso./g' \
        -e 's/未检测到 \/dev\/kvm。LXC 可用，但 KVM 虚拟机需要硬件虚拟化或嵌套虚拟化。/\/dev\/kvm was not detected. LXC is available, but KVM VMs require hardware virtualization or nested virtualization./g' \
        -e 's/正在启用内核转发配置.../Enabling kernel forwarding settings.../g' \
        -e 's/正在配置 LXC 和 KVM 服务.../Configuring LXC and KVM services.../g' \
        -e 's/服务 /Service /g' \
        -e 's/ 启动失败，将继续安装并在运行时降级处理。/ failed to start; installation will continue and runtime fallback will be used./g' \
        -e 's/未检测到 systemd 单元 /systemd unit was not detected: /g' \
        -e 's/，跳过。/; skipped./g' \
        -e 's/检测到 libvirt 传统 libvirtd 服务，已使用 libvirtd 模式。/Detected the legacy libvirt libvirtd service; using libvirtd mode./g' \
        -e 's/未检测到支持的服务管理器。CLICD 当前支持 systemd 或 OpenRC。/No supported service manager was detected. CLICD currently supports systemd or OpenRC./g' \
        -e 's/正在检查 libvirt default NAT 网络.../Checking libvirt default NAT network.../g' \
        -e 's/未找到 virsh，跳过 libvirt default NAT 网络检查。/virsh was not found; skipping the libvirt default NAT network check./g' \
        -e 's/libvirt default 网络仍未启动。请执行 virsh net-info default 查看详情。/libvirt default network is still not active. Run virsh net-info default for details./g' \
        -e 's/libvirt default NAT 网络已启用。/libvirt default NAT network is enabled./g' \
        -e 's/正在配置 subordinate UID\/GID 范围.../Configuring subordinate UID\/GID ranges.../g' \
        -e 's/根文件系统 /Root filesystem /g' \
        -e 's/ 不需要\/不适合自动启用 ext4 project quota，CLICD 将使用兼容磁盘限制模式。/ does not need or is not suitable for automatic ext4 project quota; CLICD will use compatible disk limit mode./g' \
        -e 's/ 不在自动 project quota 支持范围，CLICD 将使用兼容磁盘限制模式。/ is not supported for automatic project quota; CLICD will use compatible disk limit mode./g' \
        -e 's/根分区来源 /Root partition source /g' \
        -e 's/ 不是块设备，跳过 project quota 自动检查，CLICD 将使用兼容磁盘限制模式。/ is not a block device; skipping automatic project quota check and using compatible disk limit mode./g' \
        -e 's/未找到 tune2fs，跳过 project quota 检查，CLICD 将使用兼容磁盘限制模式。/tune2fs was not found; skipping project quota check and using compatible disk limit mode./g' \
        -e 's/检测到 ext4 project quota 已可用。/ext4 project quota is already available./g' \
        -e 's/ext4 project quota 未启用，CLICD 将自动回退到 loopback 镜像磁盘限制模式。/ext4 project quota is not enabled; CLICD will automatically fall back to loopback image disk limit mode./g' \
        -e 's/当前目录未找到 clicd 二进制，将下载发行版包。/No local clicd binary found; downloading release package./g' \
        -e 's/正在下载发行版包：/Downloading release package: /g' \
        -e 's/下载发行版包需要 curl 或 wget。/Downloading the release package requires curl or wget./g' \
        -e 's/下载的发行版包中未找到 clicd 二进制。/The downloaded release package does not contain the clicd binary./g' \
        -e 's/未找到 clicd 二进制，安装无法继续。/clicd binary was not found; installation cannot continue./g' \
        -e 's/已安装二进制：/Installed binary: /g' \
        -e 's/正在安装 CLICD 服务.../Installing CLICD service.../g' \
        -e 's/正在清理 CLICD 防火墙和网桥规则.../Cleaning CLICD firewall and bridge rules.../g' \
        -e 's/已清理 /Cleaned /g' \
        -e 's/ 中的 CLICD 配额记录/ CLICD quota records/g' \
        -e 's/跳过当前安装目录 /Skipping current installation directory /g' \
        -e 's/，避免中断后续安装步骤。/ to avoid interrupting later installation steps./g' \
        -e 's/ 被占用，终止占用进程后重试删除.../ is busy; killing occupying processes and retrying removal.../g' \
        -e 's/正在删除 CLICD 使用的 LXC 镜像缓存.../Removing LXC image cache used by CLICD.../g' \
        -e 's/正在删除 KVM 虚拟机域 /Removing KVM VM domain /g' \
        -e 's/正在销毁 CLICD 创建的 KVM 虚拟机.../Destroying CLICD-created KVM VMs.../g' \
        -e 's/检测到非 CLICD 虚拟机仍在使用 libvirt default 网络，已保留 default\/virbr0。/Non-CLICD VMs are still using the libvirt default network, so default\/virbr0 has been kept./g' \
        -e 's/正在删除 CLICD 创建的 libvirt default NAT 网络.../Removing CLICD-created libvirt default NAT network.../g' \
        -e 's/已删除 /Removed /g' \
        -e 's/检测到 /Detected /g' \
        -e 's/安装完成/Installation complete/g' \
        -e 's/Web 面板/Web panel/g' \
        -e 's/二进制/Binary/g' \
        -e 's/安装日志/Install log/g' \
        -e 's/服务/Service/g' \
        -e 's/运行日志/Runtime log/g' \
        -e 's/首次安装时的初始账号信息：/Initial account information for first installation:/g' \
        -e 's/如果没有显示密码，说明服务器已有/If no password is shown, the server already has/g' \
        -e 's/已有管理员密码使用 bcrypt 存储，无法反查；请使用面板内修改密码或重置配置。/Existing admin passwords are stored with bcrypt and cannot be recovered. Change it in the panel or reset configuration./g' \
        -e 's/：/: /g' \
        -e 's/，/, /g' \
        -e 's/。/./g' \
        -e 's/（/(/g' \
        -e 's/）/)/g' \
        -e 's/、/, /g' \
    )"
    printf '%s' "$msg"
}

echo "====================================="
echo "  $(tr_msg "CLICD 中文安装/卸载脚本")"
echo "====================================="

write_log_file() {
    if [ "$(id -u 2>/dev/null || echo 1)" = "0" ]; then
        printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || true)" "$*" >> "$LOG_FILE" 2>/dev/null || true
    fi
}

log() {
    msg="$(tr_msg "$*")"
    echo "[clicd] $msg"
    write_log_file "[clicd] $msg"
}

warn() {
    label="$(tr_msg "警告")"
    msg="$(tr_msg "$*")"
    echo "[clicd][$label] $msg" >&2
    write_log_file "[$label] $msg"
}

die() {
    label="$(tr_msg "错误")"
    msg="$(tr_msg "$*")"
    echo "[clicd][$label] $msg" >&2
    write_log_file "[$label] $msg"
    echo "" >&2
    echo "$(tr_msg "安装/卸载未完成。请查看日志：")$LOG_FILE" >&2
    echo "$(tr_msg "如果你确认这是程序问题，请提交 issue：")$ISSUE_URL" >&2
    exit 1
}

has_cmd() {
    command -v "$1" >/dev/null 2>&1
}

is_systemd() {
    has_cmd systemctl && [ -d /run/systemd/system ]
}

is_openrc() {
    has_cmd rc-service && has_cmd rc-update
}

run_step() {
    step_name="$1"
    shift
    log "开始：$step_name"
    if ( "$@" ) >> "$LOG_FILE" 2>&1; then
        log "完成：$step_name"
        return 0
    fi
    rc="$?"
    echo "" >&2
    echo "[clicd][$(tr_msg "错误")] $(tr_msg "步骤失败：")$(tr_msg "$step_name")$(tr_msg "，")$(tr_msg "退出码：")$rc" >&2
    echo "[clicd][$(tr_msg "错误")] $(tr_msg "最近 80 行日志：")$LOG_FILE" >&2
    tail -n 80 "$LOG_FILE" >&2 2>/dev/null || true
    echo "" >&2
    echo "$(tr_msg "请将上述日志和系统信息提交到：")$ISSUE_URL" >&2
    exit "$rc"
}

check_os_compatibility() {
    log "系统检测：ID=${OS_ID} ID_LIKE=${OS_LIKE} ARCH=${HOST_ARCH_RAW} CLICD_ARCH=${CLICD_ARCH_NORMALIZED:-unsupported}"
    [ -n "$CLICD_ARCH_NORMALIZED" ] || die "当前安装包支持 x86_64/amd64 和 aarch64/arm64，当前架构：${HOST_ARCH_RAW}。"
    if ! is_systemd && ! is_openrc; then
        die "未检测到 systemd 或 OpenRC，无法安装服务。"
    fi
    case "$OS_ID" in
        ubuntu|debian|alpine|centos|rhel|rocky|almalinux|fedora)
            ;;
        *)
            if ! has_cmd apt-get && ! has_cmd apk && ! has_cmd dnf && ! has_cmd yum; then
                die "暂不支持当前 Linux 发行版：${OS_ID} ${OS_LIKE}。请提交 issue 并附上 /etc/os-release。"
            fi
            warn "发行版 ${OS_ID} 不在主要支持列表，将按检测到的软件包管理器尝试安装。"
            ;;
    esac
}

check_storage_compatibility() {
    root_fs="$(findmnt -no FSTYPE / 2>/dev/null || echo unknown)"
    avail_kb="$(df -Pk / 2>/dev/null | awk 'NR==2 {print $4}' || echo 0)"
    log "存储检测：根文件系统=${root_fs} 可用空间=${avail_kb}KB"
    if [ "${avail_kb:-0}" -lt 5242880 ]; then
        warn "根分区可用空间低于 5GB，下载镜像或创建 KVM/LXC 时可能失败。"
    fi
}

if [ "$(id -u)" -ne 0 ]; then
    echo "$(tr_msg "请使用 root 权限运行：")sudo ./install.sh"
    echo "$(tr_msg "或执行：")curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh"
    echo "$(tr_msg "卸载：")curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall"
    echo "$(tr_msg "问题反馈：")$ISSUE_URL"
    exit 1
fi

: > "$LOG_FILE" 2>/dev/null || true
log "日志文件：$LOG_FILE"
log "仓库地址：https://github.com/${REPO}"
log "问题反馈：$ISSUE_URL"

OS_ID="unknown"
OS_LIKE=""
if [ -r /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_LIKE="${ID_LIKE:-}"
fi

usage() {
    if [ "$CLICD_LANG_DETECTED" = "en" ]; then
        cat << EOF
Usage:
  ./install.sh              Install or upgrade CLICD
  ./install.sh uninstall    Uninstall CLICD (removes containers, VMs, image cache, and config data)

Environment variables:
  CLICD_REPO=owner/repo          Default: ${REPO}
  CLICD_VERSION=latest|v1.0.0    Default: latest
  CLICD_LANG=en|zh               Default: auto
  CLICD_LOG_FILE=/path/file.log  Default: ${LOG_FILE}

Examples:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall --yes

Log: ${LOG_FILE}
Issues: ${ISSUE_URL}
EOF
        return
    fi
    cat << EOF
用法：
  ./install.sh              安装或升级 CLICD
  ./install.sh uninstall    卸载 CLICD（会删除容器、虚拟机、镜像缓存和配置数据）

环境变量：
  CLICD_REPO=owner/repo          默认：${REPO}
  CLICD_VERSION=latest|v1.0.0    默认：latest
  CLICD_LANG=en|zh               默认：自动检测
  CLICD_LOG_FILE=/path/file.log  默认：${LOG_FILE}

示例：
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | sudo sh -s -- uninstall --yes

日志：${LOG_FILE}
问题反馈：${ISSUE_URL}
EOF
}

remove_path() {
    path="$1"
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then
        return
    fi
    rm -rf "$path"
    log "已删除 $path"
}

unmount_path_tree() {
    path="$1"
    if [ ! -e "$path" ]; then
        return
    fi

    if has_cmd findmnt; then
        findmnt -R -n -o TARGET "$path" 2>/dev/null | sort -r | while IFS= read -r mountpoint; do
            [ -n "$mountpoint" ] || continue
            umount -R -l "$mountpoint" >/dev/null 2>&1 || umount -l "$mountpoint" >/dev/null 2>&1 || true
        done
    fi

    umount -R -l "$path/rootfs" >/dev/null 2>&1 || umount -l "$path/rootfs" >/dev/null 2>&1 || true
    umount -R -l "$path" >/dev/null 2>&1 || umount -l "$path" >/dev/null 2>&1 || true
}

detach_container_loop_devices() {
    path="$1"
    if ! has_cmd losetup; then
        return
    fi

    for image in "$path"/rootfs.img "$path"/*.img; do
        [ -e "$image" ] || continue
        losetup -j "$image" 2>/dev/null | sed 's/:.*//' | while IFS= read -r loopdev; do
            [ -n "$loopdev" ] || continue
            losetup -d "$loopdev" >/dev/null 2>&1 || true
        done
    done
}

kill_path_users() {
    path="$1"
    if has_cmd fuser && [ -e "$path" ]; then
        fuser -km "$path" >/dev/null 2>&1 || true
    fi
}

remove_lxc_container_dir() {
    container_dir="$1"
    container_name="$(basename "$container_dir")"

    if has_cmd lxc-stop; then
        lxc-stop -n "$container_name" -k >/dev/null 2>&1 || true
    fi
    if has_cmd lxc-destroy; then
        lxc-destroy -n "$container_name" -f >/dev/null 2>&1 || true
    fi

    unmount_path_tree "$container_dir"
    detach_container_loop_devices "$container_dir"

    if rm -rf "$container_dir" >/dev/null 2>&1; then
        log "已删除 $container_dir"
        return
    fi

    log "检测到 $container_dir 被占用，终止占用进程后重试删除..."
    kill_path_users "$container_dir/rootfs"
    kill_path_users "$container_dir"
    unmount_path_tree "$container_dir"
    detach_container_loop_devices "$container_dir"
    rm -rf "$container_dir"
    log "已删除 $container_dir"
}

remove_clicd_lxc_image_cache() {
    log "正在删除 CLICD 使用的 LXC 镜像缓存..."

    for container_dir in /var/lib/lxc/clicd-img-dl-*; do
        [ -d "$container_dir" ] || continue
        remove_lxc_container_dir "$container_dir"
    done

    for image in \
        "ubuntu noble amd64" \
        "ubuntu jammy amd64" \
        "debian trixie amd64" \
        "debian bookworm amd64" \
        "debian bullseye amd64" \
        "alpine 3.21 amd64" \
        "centos 9-Stream amd64" \
        "archlinux current amd64" \
        "fedora 44 amd64" \
        "rockylinux 10 amd64" \
        "ubuntu noble arm64" \
        "ubuntu jammy arm64" \
        "debian trixie arm64" \
        "debian bookworm arm64" \
        "debian bullseye arm64" \
        "alpine 3.21 arm64" \
        "centos 9-Stream arm64" \
        "archlinux current arm64" \
        "fedora 44 arm64" \
        "rockylinux 10 arm64"
    do
        set -- $image
        distro="$1"
        release="$2"
        arch="$3"
        cache_dir="/var/cache/lxc/download/$distro/$release/$arch"
        remove_path "$cache_dir"
        rmdir "/var/cache/lxc/download/$distro/$release" >/dev/null 2>&1 || true
        rmdir "/var/cache/lxc/download/$distro" >/dev/null 2>&1 || true
    done

    rmdir /var/cache/lxc/download >/dev/null 2>&1 || true
    rmdir /var/cache/lxc >/dev/null 2>&1 || true
}

remove_kvm_domain() {
    domain="$1"
    case "$domain" in
        vm-[0-9]*)
            ;;
        *)
            return
            ;;
    esac
    suffix="${domain#vm-}"
    case "$suffix" in
        ""|*[!0-9]*)
            return
            ;;
    esac
    if [ ! -d "/var/lib/clicd/kvm/instances/$domain" ] &&
        ! virsh dumpxml "$domain" 2>/dev/null | grep -q '/var/lib/clicd/kvm/'; then
        return
    fi

    log "正在删除 KVM 虚拟机域 $domain..."
    virsh destroy "$domain" >/dev/null 2>&1 || true
    virsh undefine "$domain" --remove-all-storage --nvram >/dev/null 2>&1 ||
        virsh undefine "$domain" --nvram >/dev/null 2>&1 ||
        virsh undefine "$domain" >/dev/null 2>&1 ||
        true
}

destroy_clicd_kvm_domains() {
    if ! has_cmd virsh; then
        return
    fi

    log "正在销毁 CLICD 创建的 KVM 虚拟机..."
    virsh list --all --name 2>/dev/null | while IFS= read -r domain; do
        [ -n "$domain" ] || continue
        remove_kvm_domain "$domain"
    done
}

domain_is_clicd_kvm() {
    domain="$1"
    case "$domain" in
        vm-[0-9]*)
            return 0
            ;;
    esac
    virsh dumpxml "$domain" 2>/dev/null | grep -q '/var/lib/clicd/kvm/'
}

libvirt_default_used_by_non_clicd_domain() {
    if ! has_cmd virsh; then
        return 1
    fi

    for domain in $(virsh list --all --name 2>/dev/null); do
        [ -n "$domain" ] || continue
        if domain_is_clicd_kvm "$domain"; then
            continue
        fi
        if virsh domiflist "$domain" 2>/dev/null | awk '$3 == "default" || $3 == "virbr0" {found = 1} END {exit found ? 0 : 1}'; then
            return 0
        fi
    done
    return 1
}

remove_clicd_libvirt_default_network() {
    if ! has_cmd virsh || [ ! -f "$LIBVIRT_DEFAULT_MARKER" ]; then
        return
    fi
    if libvirt_default_used_by_non_clicd_domain; then
        warn "检测到非 CLICD 虚拟机仍在使用 libvirt default 网络，已保留 default/virbr0。"
        return
    fi

    log "正在删除 CLICD 创建的 libvirt default NAT 网络..."
    virsh net-destroy default >/dev/null 2>&1 || true
    virsh net-undefine default >/dev/null 2>&1 || true
    rm -f "$LIBVIRT_DEFAULT_MARKER"
}

delete_iptables_lines() {
    table="$1"
    chain="$2"
    pattern="$3"
    if ! has_cmd iptables; then
        return
    fi

    while :; do
        line="$(iptables -t "$table" -L "$chain" -n --line-numbers 2>/dev/null | awk -v pat="$pattern" '$0 ~ pat {print $1; exit}')"
        [ -n "$line" ] || break
        iptables -t "$table" -D "$chain" "$line" >/dev/null 2>&1 || break
    done
}

delete_iptables_rule() {
    table="$1"
    shift
    if ! has_cmd iptables; then
        return
    fi

    while iptables -t "$table" -D "$@" >/dev/null 2>&1; do
        :
    done
}

delete_filter_rule() {
    if ! has_cmd iptables; then
        return
    fi

    while iptables -D "$@" >/dev/null 2>&1; do
        :
    done
}

delete_ip6_filter_rule() {
    if ! has_cmd ip6tables; then
        return
    fi

    while ip6tables -D "$@" >/dev/null 2>&1; do
        :
    done
}

delete_ip6tables_nat_source() {
    source="$1"
    if ! has_cmd ip6tables || [ -z "$source" ]; then
        return
    fi

    while :; do
        rule="$(
            ip6tables -t nat -S POSTROUTING 2>/dev/null |
                grep -F -- "-s $source" |
                grep -F -- " -j MASQUERADE" |
                sed 's/^-A /-D /' |
                head -n 1
        )"
        [ -n "$rule" ] || break
        # shellcheck disable=SC2086
        ip6tables -t nat $rule >/dev/null 2>&1 || break
    done
}

read_clicd_network_records() {
    db="/root/.clicd/config.db"
    legacy="/root/.clicd/config.json"
    query="SELECT COALESCE(virtualization,''), COALESCE(ipv6,''), COALESCE(ipv6_interface,''), COALESCE(mac_address,'') FROM containers WHERE COALESCE(ipv6,'') <> '' OR COALESCE(mac_address,'') <> '';"

    if [ -f "$db" ] && has_cmd sqlite3; then
        sqlite3 -separator '|' "$db" "$query" 2>/dev/null || true
    elif [ -f "$db" ] && has_cmd python3; then
        CLICD_DB="$db" python3 - <<'PY' 2>/dev/null || true
import os
import sqlite3

db = os.environ.get("CLICD_DB")
for row in sqlite3.connect(db).execute(
    "SELECT COALESCE(virtualization,''), COALESCE(ipv6,''), COALESCE(ipv6_interface,''), COALESCE(mac_address,'') "
    "FROM containers WHERE COALESCE(ipv6,'') <> '' OR COALESCE(mac_address,'') <> ''"
):
    print("|".join("" if value is None else str(value) for value in row))
PY
    fi

    if [ -f "$legacy" ] && has_cmd python3; then
        CLICD_LEGACY_CONFIG="$legacy" python3 - <<'PY' 2>/dev/null || true
import json
import os

path = os.environ.get("CLICD_LEGACY_CONFIG")
with open(path, "r", encoding="utf-8") as f:
    data = json.load(f)
for item in data.get("containers", []):
    virt = item.get("virtualization", "")
    ipv6 = item.get("ipv6", "")
    uplink = item.get("ipv6_interface", "")
    mac = item.get("mac_address", "")
    if ipv6 or mac:
        print("|".join(str(value or "") for value in (virt, ipv6, uplink, mac)))
PY
    fi
}

cleanup_clicd_ipv6_record() {
    virt="$1"
    ipv6="$2"
    uplink="$3"
    mac="$4"
    bridge="lxcbr0"
    if [ "$virt" = "kvm" ]; then
        bridge="virbr0"
    fi
    mac="$(printf '%s' "$mac" | tr '[:upper:]' '[:lower:]')"

    if [ -n "$mac" ] && [ "$bridge" = "virbr0" ]; then
        delete_ip6_filter_rule FORWARD -i "$bridge" -m mac --mac-source "$mac" -j DROP
    fi

    [ -n "$ipv6" ] || return
    addr="${ipv6%%/*}"
    source="$ipv6"
    case "$source" in
        */*) ;;
        *) source="$source/128" ;;
    esac

    delete_ip6tables_nat_source "$source"
    delete_ip6_filter_rule FORWARD -i "$bridge" -s "$source" -j ACCEPT
    delete_ip6_filter_rule FORWARD -o "$bridge" -d "$source" -j ACCEPT
    if [ -n "$mac" ] && [ "$bridge" = "virbr0" ]; then
        delete_ip6_filter_rule FORWARD -i "$bridge" -m mac --mac-source "$mac" -s "$source" -j ACCEPT
        delete_ip6_filter_rule FORWARD -i "$bridge" -m mac --mac-source "$mac" -j DROP
    fi

    if has_cmd ip; then
        ip -6 route del "$source" dev "$bridge" >/dev/null 2>&1 || true
        if [ -n "$uplink" ]; then
            ip -6 neigh del proxy "$addr" dev "$uplink" >/dev/null 2>&1 || true
        fi
    fi
}

cleanup_clicd_ipv6_from_config() {
    read_clicd_network_records | while IFS='|' read -r virt ipv6 uplink mac; do
        cleanup_clicd_ipv6_record "$virt" "$ipv6" "$uplink" "$mac"
    done
}

cleanup_clicd_ipv6_bridge_routes() {
    if ! has_cmd ip; then
        return
    fi

    for bridge in lxcbr0 virbr0; do
        ip -6 route show dev "$bridge" 2>/dev/null | awk '$1 ~ /\/128$/ {print $1}' | while IFS= read -r source; do
            [ -n "$source" ] || continue
            addr="${source%%/*}"
            delete_ip6tables_nat_source "$source"
            delete_ip6_filter_rule FORWARD -i "$bridge" -s "$source" -j ACCEPT
            delete_ip6_filter_rule FORWARD -o "$bridge" -d "$source" -j ACCEPT
            ip -6 neigh show proxy 2>/dev/null | awk -v addr="$addr" '$1 == addr {for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1)}' | while IFS= read -r uplink; do
                [ -n "$uplink" ] || continue
                ip -6 neigh del proxy "$addr" dev "$uplink" >/dev/null 2>&1 || true
            done
            ip -6 route del "$source" dev "$bridge" >/dev/null 2>&1 || true
        done
        ip -6 addr del fe80::1/64 dev "$bridge" >/dev/null 2>&1 || true
    done
}

delete_ip6tables_bridge_rules() {
    if ! has_cmd ip6tables; then
        return
    fi

    for bridge in lxcbr0 virbr0; do
        while :; do
            rule="$(ip6tables -S FORWARD 2>/dev/null | grep -- "$bridge" | sed 's/^-A /-D /' | head -n 1)"
            [ -n "$rule" ] || break
            # shellcheck disable=SC2086
            ip6tables $rule >/dev/null 2>&1 || break
        done
    done
}

cleanup_clicd_networking() {
    log "正在清理 CLICD 防火墙和网桥规则..."
    delete_iptables_lines nat PREROUTING 'clicd-'
    delete_iptables_rule nat POSTROUTING -s 10.0.3.0/24 -o eth+ -j MASQUERADE
    delete_iptables_rule nat POSTROUTING -s 192.168.122.0/24 -o eth+ -j MASQUERADE
    cleanup_clicd_ipv6_from_config
    cleanup_clicd_ipv6_bridge_routes

    for bridge in lxcbr0 virbr0; do
        delete_filter_rule FORWARD -i "$bridge" -j ACCEPT
        delete_filter_rule FORWARD -o "$bridge" -j ACCEPT
        delete_filter_rule FORWARD -i "$bridge" -o "$bridge" -j ACCEPT
    done
    delete_ip6tables_bridge_rules
}

remove_clicd_host_hooks() {
    if has_cmd systemctl; then
        systemctl stop clicd-kvm-ipv6.service >/dev/null 2>&1 || true
        systemctl disable clicd-kvm-ipv6.service >/dev/null 2>&1 || true
    fi
    if has_cmd rc-service; then
        rc-service clicd-kvm-ipv6 stop >/dev/null 2>&1 || true
    fi
    if has_cmd rc-update; then
        rc-update del clicd-kvm-ipv6 default >/dev/null 2>&1 || true
    fi

    remove_path /usr/local/sbin/clicd-kvm-ipv6-init
    remove_path /etc/systemd/system/clicd-kvm-ipv6.service
    remove_path /etc/local.d/clicd-kvm-ipv6.start
    remove_path /etc/network/if-up.d/clicd-kvm-ipv6
}

remove_clicd_quota_records() {
    for file in /etc/projects /etc/projid; do
        [ -f "$file" ] || continue
        tmp="${file}.clicd-clean.$$"
        grep -v 'clicd-' "$file" > "$tmp" || true
        cat "$tmp" > "$file"
        rm -f "$tmp"
        log "已清理 $file 中的 CLICD 配额记录"
    done
}

remove_clicd_tmp_files() {
    current_dir="$(pwd -P 2>/dev/null || pwd)"
    for path in /tmp/clicd-* /tmp/clicd.*; do
        [ -e "$path" ] || [ -L "$path" ] || continue
        abs_path="$(cd "$(dirname "$path")" 2>/dev/null && pwd -P)/$(basename "$path")"
        if [ "$abs_path" = "$current_dir" ]; then
            log "跳过当前安装目录 $path，避免中断后续安装步骤。"
            continue
        fi
        rm -rf "$path"
        log "已删除 $path"
    done
}

remove_clicd_swapfile() {
    if [ ! -e /swapfile ]; then
        return
    fi
    swapoff /swapfile >/dev/null 2>&1 || true
    remove_path /swapfile
}


confirm_uninstall() {
    if [ "${CLICD_UNINSTALL_CONFIRM:-}" = "1" ] || [ "${CLICD_UNINSTALL_CONFIRM:-}" = "yes" ] || [ "$ACTION_CONFIRM" = "--yes" ] || [ "$ACTION_CONFIRM" = "-y" ]; then
        return
    fi
    echo ""
    echo "[clicd][$(tr_msg "警告")] $(tr_msg "卸载会停止并删除 CLICD 服务、配置数据库、CLICD 创建的 LXC/KVM 实例和缓存数据。")" >&2
    echo "[clicd][$(tr_msg "警告")] $(tr_msg "为避免误删生产数据，脚本只会删除名称形如 ct-数字 的 LXC 容器、clicd-img-dl-* 下载临时容器和 vm-数字 的 KVM 域。")" >&2
    echo "$(tr_msg "如需确认卸载，请输入：YES")" >&2
    if [ -r /dev/tty ]; then
        IFS= read -r answer < /dev/tty
    elif [ -t 0 ]; then
        IFS= read -r answer
    else
        answer=""
    fi
    if [ "$answer" != "YES" ]; then
        die "已取消卸载。如需非交互卸载，请设置 CLICD_UNINSTALL_CONFIRM=1。"
    fi
}

uninstall_clicd() {
    confirm_uninstall
    log "正在卸载 CLICD..."

    if has_cmd systemctl; then
        systemctl stop clicd >/dev/null 2>&1 || true
        systemctl disable clicd >/dev/null 2>&1 || true
    fi

    if has_cmd rc-service; then
        rc-service clicd stop >/dev/null 2>&1 || true
    fi
    if has_cmd rc-update; then
        rc-update del clicd default >/dev/null 2>&1 || true
    fi

    log "正在删除 CLICD 创建的 LXC 容器（/var/lib/lxc/ct-数字）..."
    for container_dir in /var/lib/lxc/ct-[0-9]*; do
        [ -d "$container_dir" ] || continue
        remove_lxc_container_dir "$container_dir"
    done
    remove_clicd_lxc_image_cache
    destroy_clicd_kvm_domains
    remove_clicd_libvirt_default_network
    cleanup_clicd_networking
    remove_clicd_host_hooks
    remove_clicd_quota_records

    remove_path /etc/systemd/system/clicd.service
    remove_path /etc/init.d/clicd
    remove_path /usr/local/bin/clicd
    remove_path /etc/sysctl.d/99-clicd.conf
    remove_path /var/log/clicd.log
    remove_path /var/log/clicd.err
    remove_path /root/.clicd
    # /var/lib/lxc 可能包含非 CLICD 容器，生产环境不整体删除。
    unmount_path_tree /var/lib/clicd
    remove_path /var/lib/clicd
    # /var/cache/lxc 是 LXC 全局缓存，已按 CLICD 模板精确清理，生产环境不整体删除。
    remove_path /var/cache/clicd
    warn "保留 /root/clicd-backups，避免误删部署/回滚备份。确认不需要后可手动删除。"
    remove_clicd_tmp_files
    remove_clicd_swapfile

    if has_cmd systemctl; then
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl reset-failed clicd >/dev/null 2>&1 || true
    fi
    if has_cmd sysctl; then
        sysctl --system >/dev/null 2>&1 || true
    fi

    echo ""
    echo "====================================="
    echo "  $(tr_msg "CLICD 卸载完成")"
    echo "====================================="
    echo "  $(tr_msg "已删除服务、二进制、SQLite/配置数据、CLICD LXC/KVM 实例、")"
    echo "  $(tr_msg "CLICD 镜像缓存、防火墙规则、主机钩子、配额记录和临时文件。")"
    echo "  $(tr_msg "已保留 /root/clicd-backups 和非 CLICD 的 LXC 全局缓存，避免误删生产备份/共享镜像。")"
    echo "  $(tr_msg "日志：")$LOG_FILE"
    echo "  $(tr_msg "问题反馈：")$ISSUE_URL"
    echo "====================================="
}

case "$ACTION" in
    install|"")
        ;;
    uninstall|remove)
        uninstall_clicd
        exit 0
        ;;
    -h|--help|help)
        usage
        exit 0
        ;;
    *)
        die "未知操作：$ACTION"
        ;;
esac

install_apk() {
    log "正在使用 apk 安装依赖..."
    apk update
    apk add --no-cache \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-download \
        lxc-openrc \
        lxc-bridge \
        lxc-templates \
        bridge-utils \
        iproute2 \
        iptables \
        dnsmasq \
        dbus

    if kvm_supported_arch; then
        apk add --no-cache \
        "$(qemu_system_package_apk)" \
        qemu-img \
        libvirt \
        libvirt-daemon \
        libvirt-client \
        libvirt-qemu
    else
        warn_kvm_unsupported_arch
    fi

    for pkg in lxcfs shadow conntrack-tools quota-tools e2fsprogs xfsprogs cloud-utils genisoimage xorriso smartmontools "$(qemu_efi_package_apk)"; do
        apk add --no-cache "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_apt() {
    log "正在使用 apt 安装依赖..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz-utils \
        lxc \
        lxc-templates \
        lxcfs \
        bridge-utils \
        uidmap \
        iproute2 \
        iptables \
        conntrack \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq-base

    if kvm_supported_arch; then
        if [ "$CLICD_ARCH_NORMALIZED" = "arm64" ]; then
            apt-get install -y \
                "$(qemu_system_package_apt)" \
                qemu-utils \
                libvirt-daemon-system \
                libvirt-clients \
                cloud-image-utils \
                genisoimage \
                xorriso \
                smartmontools \
                virtinst \
                "$(qemu_efi_package_apt)"
        else
            apt-get install -y \
                qemu-kvm \
                "$(qemu_system_package_apt)" \
                qemu-utils \
                libvirt-daemon-system \
                libvirt-clients \
                cloud-image-utils \
                genisoimage \
                xorriso \
                smartmontools \
                virtinst \
                "$(qemu_efi_package_apt)"
        fi
    else
        warn_kvm_unsupported_arch
        apt-get install -y qemu-utils genisoimage xorriso smartmontools >/dev/null 2>&1 || true
    fi
}

enable_el_repos() {
    if has_cmd dnf; then
        dnf install -y 'dnf-command(config-manager)' >/dev/null 2>&1 || true
        dnf install -y epel-release || true
        dnf config-manager --set-enabled crb >/dev/null 2>&1 || true
        dnf config-manager --set-enabled powertools >/dev/null 2>&1 || true
    elif has_cmd yum; then
        yum install -y yum-utils >/dev/null 2>&1 || true
        yum install -y epel-release || true
        yum-config-manager --enable powertools >/dev/null 2>&1 || true
    fi
}

install_dnf() {
    log "正在使用 dnf 安装依赖..."
    enable_el_repos
    dnf install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-templates \
        bridge-utils \
        iproute \
        iptables \
        conntrack-tools \
        shadow-utils \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq

    if kvm_supported_arch; then
        dnf install -y \
        "$(qemu_system_package_rpm)" \
        qemu-img \
        libvirt \
        libvirt-daemon-kvm \
        libvirt-client \
        virt-install \
        cloud-utils \
        genisoimage
    else
        warn_kvm_unsupported_arch
        dnf install -y qemu-img genisoimage >/dev/null 2>&1 || true
    fi

    for pkg in lxcfs xorriso "$(qemu_efi_package_rpm)" smartmontools; do
        dnf install -y "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_yum() {
    log "正在使用 yum 安装依赖..."
    enable_el_repos
    yum install -y \
        ca-certificates \
        curl \
        wget \
        tar \
        gzip \
        xz \
        lxc \
        lxc-templates \
        bridge-utils \
        iproute \
        iptables \
        conntrack-tools \
        shadow-utils \
        quota \
        e2fsprogs \
        xfsprogs \
        dnsmasq

    if kvm_supported_arch; then
        yum install -y \
        "$(qemu_system_package_rpm)" \
        qemu-img \
        libvirt \
        libvirt-daemon-kvm \
        libvirt-client \
        virt-install \
        cloud-utils \
        genisoimage
    else
        warn_kvm_unsupported_arch
        yum install -y qemu-img genisoimage >/dev/null 2>&1 || true
    fi

    for pkg in lxcfs xorriso "$(qemu_efi_package_rpm)" smartmontools; do
        yum install -y "$pkg" >/dev/null 2>&1 || warn "可选依赖未安装：$pkg"
    done
}

install_dependencies() {
    case "$OS_ID" in
        ubuntu|debian)
            install_apt
            ;;
        alpine)
            install_apk
            ;;
        centos|rhel|rocky|almalinux|fedora)
            if has_cmd dnf; then
                install_dnf
            elif has_cmd yum; then
                install_yum
            else
                die "当前系统 $OS_ID 未找到 dnf/yum，无法安装依赖。"
            fi
            ;;
        *)
            if has_cmd apt-get; then
                install_apt
            elif has_cmd apk; then
                install_apk
            elif has_cmd dnf; then
                install_dnf
            elif has_cmd yum; then
                install_yum
            else
                die "暂不支持当前 Linux 发行版：${OS_ID} ${OS_LIKE}。请提交 issue 并附上 /etc/os-release。"
            fi
            ;;
    esac

    has_cmd lxc-create || die "依赖安装后仍未找到 lxc-create，请检查 LXC 软件源/安装日志。"
    has_cmd iptables || die "依赖安装后仍未找到 iptables，请检查系统网络工具包。"
    has_cmd ip || die "依赖安装后仍未找到 ip 命令，请检查 iproute2 安装。"
    if kvm_supported_arch; then
        has_cmd virsh || die "依赖安装后仍未找到 virsh，请检查 libvirt-client/libvirt-clients 安装。"
        has_cmd "$(qemu_emulator_cmd)" || die "依赖安装后仍未找到 $(qemu_emulator_cmd)，请检查 QEMU 安装。"
        has_cmd qemu-img || die "依赖安装后仍未找到 qemu-img，请检查 qemu-utils/qemu-img 安装。"
        has_cmd cloud-localds || die "依赖安装后仍未找到 cloud-localds，请检查 cloud-image-utils/cloud-utils 安装。"
        if ! has_cmd genisoimage && ! has_cmd mkisofs && ! has_cmd xorriso; then
            die "Windows KVM 初始化需要 genisoimage、mkisofs 或 xorriso 中任意一个。"
        fi
        if [ ! -e /dev/kvm ]; then
            warn "未检测到 /dev/kvm。LXC 可用，但 KVM 虚拟机需要硬件虚拟化或嵌套虚拟化。"
        fi
    else
        warn_kvm_unsupported_arch
    fi
}

configure_kernel_networking() {
    log "正在启用内核转发配置..."
    cat > /etc/sysctl.d/99-clicd.conf << 'EOF'
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.bridge.bridge-nf-call-iptables = 0
net.bridge.bridge-nf-call-ip6tables = 0
EOF

    modprobe br_netfilter >/dev/null 2>&1 || true
    sysctl --system >/dev/null 2>&1 || true
}

systemd_unit_exists() {
    unit="$1"
    systemctl list-unit-files "$unit" >/dev/null 2>&1 || [ -e "/etc/systemd/system/$unit" ] || [ -e "/usr/lib/systemd/system/$unit" ] || [ -e "/lib/systemd/system/$unit" ]
}

systemd_enable_now_if_exists() {
    unit="$1"
    if systemd_unit_exists "$unit"; then
        systemctl enable --now "$unit" >/dev/null 2>&1 || warn "服务 $unit 启动失败，将继续安装并在运行时降级处理。"
        return
    fi
    log "未检测到 systemd 单元 $unit，跳过。"
}

systemd_existing_units() {
    for unit in "$@"; do
        if systemd_unit_exists "$unit"; then
            printf ' %s' "$unit"
        fi
    done
}

setup_runtime_services() {
    log "正在配置 LXC 和 KVM 服务..."

    if is_systemd; then
        systemd_enable_now_if_exists lxcfs.service
        systemd_enable_now_if_exists lxc-net.service
        systemd_enable_now_if_exists lxc.service
        if systemd_unit_exists libvirtd.service; then
            systemd_enable_now_if_exists libvirtd.service
            log "检测到 libvirt 传统 libvirtd 服务，已使用 libvirtd 模式。"
        else
            systemd_enable_now_if_exists virtqemud.service
            systemd_enable_now_if_exists virtqemud.socket
        fi
        systemd_enable_now_if_exists virtlogd.socket
        return
    fi

    if is_openrc; then
        rc-update add cgroups default >/dev/null 2>&1 || true
        rc-service cgroups start >/dev/null 2>&1 || true
        rc-update add lxc default >/dev/null 2>&1 || true
        rc-service lxc start >/dev/null 2>&1 || true
        rc-update add lxcfs default >/dev/null 2>&1 || true
        rc-service lxcfs start >/dev/null 2>&1 || true
        rc-update add dbus default >/dev/null 2>&1 || true
        rc-service dbus start >/dev/null 2>&1 || true
        rc-update add libvirtd default >/dev/null 2>&1 || true
        rc-service libvirtd start >/dev/null 2>&1 || true
        rc-update add virtlogd default >/dev/null 2>&1 || true
        rc-service virtlogd start >/dev/null 2>&1 || true
        return
    fi

    die "未检测到支持的服务管理器。CLICD 当前支持 systemd 或 OpenRC。"
}


libvirt_network_active() {
    LC_ALL=C LANG=C virsh net-info default 2>/dev/null \
        | awk -F: '$1 ~ /^[[:space:]]*Active[[:space:]]*$/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print tolower($2)}' \
        | grep -qx yes
}

setup_default_libvirt_network() {
    if ! has_cmd virsh; then
        warn "未找到 virsh，跳过 libvirt default NAT 网络检查。"
        return
    fi
    log "正在检查 libvirt default NAT 网络..."
    if ! virsh net-info default >/dev/null 2>&1; then
        net_xml="$(mktemp /tmp/clicd-default-net.XXXXXX.xml)"
        cat > "$net_xml" << 'EOF'
<network>
  <name>default</name>
  <bridge name='virbr0'/>
  <forward mode='nat'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
EOF
        virsh net-define "$net_xml"
        rm -f "$net_xml"
        mkdir -p "$(dirname "$LIBVIRT_DEFAULT_MARKER")"
        touch "$LIBVIRT_DEFAULT_MARKER"
    fi
    if ! libvirt_network_active; then
        if ! start_output="$(LC_ALL=C LANG=C virsh net-start default 2>&1)"; then
            # Another process may have activated the network after our check.
            if ! libvirt_network_active; then
                printf '%s\n' "$start_output" >&2
                die "libvirt default 网络仍未启动。请执行 virsh net-info default 查看详情。"
            fi
        fi
    fi
    virsh net-autostart default >/dev/null
    if ! libvirt_network_active; then
        die "libvirt default 网络仍未启动。请执行 virsh net-info default 查看详情。"
    fi
    log "libvirt default NAT 网络已启用。"
}

setup_subids() {
    log "正在配置 subordinate UID/GID 范围..."
    touch /etc/subuid /etc/subgid
    grep -q '^root:' /etc/subuid 2>/dev/null || echo 'root:100000:65536' >> /etc/subuid
    grep -q '^root:' /etc/subgid 2>/dev/null || echo 'root:100000:65536' >> /etc/subgid
}

configure_lxc_storage_access() {
    log "Configuring LXC storage directory permissions..."
    mkdir -p /var/lib/lxc
    chmod 755 /var/lib/lxc
}

try_enable_project_quota() {
    root_src="$(findmnt -no SOURCE / 2>/dev/null || true)"
    root_fs="$(findmnt -no FSTYPE / 2>/dev/null || true)"

    case "$root_fs" in
        ext4)
            ;;
        xfs|btrfs|zfs|overlay|unknown|"")
            log "根文件系统 ${root_fs:-unknown} 不需要/不适合自动启用 ext4 project quota，CLICD 将使用兼容磁盘限制模式。"
            return
            ;;
        *)
            log "根文件系统 ${root_fs:-unknown} 不在自动 project quota 支持范围，CLICD 将使用兼容磁盘限制模式。"
            return
            ;;
    esac

    if [ -z "$root_src" ] || [ ! -b "$root_src" ]; then
        log "根分区来源 ${root_src:-unknown} 不是块设备，跳过 project quota 自动检查，CLICD 将使用兼容磁盘限制模式。"
        return
    fi

    if ! has_cmd tune2fs; then
        log "未找到 tune2fs，跳过 project quota 检查，CLICD 将使用兼容磁盘限制模式。"
        return
    fi

    if tune2fs -l "$root_src" 2>/dev/null | grep -q 'project'; then
        log "检测到 ext4 project quota 已可用。"
        return
    fi

    log "ext4 project quota 未启用，CLICD 将自动回退到 loopback 镜像磁盘限制模式。"
}

download_file() {
    url="$1"
    dest="$2"
    rm -f "$dest"

    if has_cmd curl; then
        curl -fL --retry 6 --retry-delay 2 --connect-timeout 20 --max-time 600 "$url" -o "$dest"
        return
    fi
    if has_cmd wget; then
        wget --tries=6 --timeout=30 --waitretry=2 -O "$dest" "$url"
        return
    fi
    return 127
}

release_api_json() {
    api_url="https://api.github.com/repos/${REPO}/releases/latest"

    if has_cmd curl; then
        curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 --max-time 120 "$api_url" 2>/dev/null || true
        return
    fi
    if has_cmd wget; then
        wget -qO- --tries=3 --timeout=30 "$api_url" 2>/dev/null || true
        return
    fi
}

release_asset_url() {
    asset_name="$1"

    if [ "$CLICD_INSTALL_VERSION" != "latest" ]; then
        printf '%s\n' "https://github.com/${REPO}/releases/download/${CLICD_INSTALL_VERSION}/${asset_name}"
        return
    fi

    api_data="$(release_api_json)"
    url="$(printf '%s\n' "$api_data" | sed -n 's/.*"browser_download_url": *"\([^"]*\/'"$asset_name"'\)".*/\1/p' | head -n 1)"
    if [ -n "$url" ]; then
        printf '%s\n' "$url"
        return
    fi

    tag="$(printf '%s\n' "$api_data" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
    if [ -n "$tag" ]; then
        printf '%s\n' "https://github.com/${REPO}/releases/download/${tag}/${asset_name}"
        return
    fi

    printf '%s\n' "https://github.com/${REPO}/releases/latest/download/${asset_name}"
}

download_release_if_needed() {
    if [ -f "./clicd" ]; then
        return
    fi

    if [ "$CLICD_INSTALL_VERSION" = "latest" ]; then
        download_url="https://github.com/${REPO}/releases/latest/download/${ASSET}"
    else
        download_url="https://github.com/${REPO}/releases/download/${CLICD_INSTALL_VERSION}/${ASSET}"
    fi

    log "当前目录未找到 clicd 二进制，将下载发行版包。"
    log "正在下载发行版包：${download_url}"

    tmp_dir="$(mktemp -d)"
    rm -f "$INSTALL_DOWNLOAD_MARKER"
    printf '%s\n' "$tmp_dir" > "$INSTALL_DOWNLOAD_MARKER" || die "Failed to write install temp marker."

    if ! has_cmd curl && ! has_cmd wget; then
        die "下载发行版包需要 curl 或 wget。"
    fi

    archive_path="$tmp_dir/$ASSET"
    archive_urls="$download_url"
    resolved_archive_url="$(release_asset_url "$ASSET")"
    if [ "$resolved_archive_url" != "$download_url" ]; then
        archive_urls="$archive_urls $resolved_archive_url"
    fi

    archive_ok=0
    for url in $archive_urls; do
        [ -n "$url" ] || continue
        log "Trying release archive: $url"
        if download_file "$url" "$archive_path" && [ -s "$archive_path" ]; then
            archive_ok=1
            break
        fi
        warn "Release archive download failed, trying next source: $url"
    done

    if [ "$archive_ok" = "1" ]; then
        tar -xzf "$archive_path" -C "$tmp_dir" || die "Failed to extract release package: $archive_path"
    else
        binary_asset="$BINARY_ASSET"
        if [ "$CLICD_INSTALL_VERSION" = "latest" ]; then
            binary_url="https://github.com/${REPO}/releases/latest/download/${binary_asset}"
        else
            binary_url="https://github.com/${REPO}/releases/download/${CLICD_INSTALL_VERSION}/${binary_asset}"
        fi
        binary_urls="$binary_url"
        resolved_binary_url="$(release_asset_url "$binary_asset")"
        if [ "$resolved_binary_url" != "$binary_url" ]; then
            binary_urls="$binary_urls $resolved_binary_url"
        fi

        binary_path="$tmp_dir/$binary_asset"
        binary_ok=0
        for url in $binary_urls; do
            [ -n "$url" ] || continue
            log "Trying release binary: $url"
            if download_file "$url" "$binary_path" && [ -s "$binary_path" ]; then
                mkdir -p "$tmp_dir/$ASSET_DIR"
                cp "$binary_path" "$tmp_dir/$ASSET_DIR/clicd"
                chmod +x "$tmp_dir/$ASSET_DIR/clicd"
                binary_ok=1
                break
            fi
            warn "Release binary download failed, trying next source: $url"
        done

        [ "$binary_ok" = "1" ] || die "Release package download failed: $download_url"
    fi

    [ -d "$tmp_dir/$ASSET_DIR" ] || die "Release package layout is invalid: missing $ASSET_DIR directory"
    [ -f "$tmp_dir/$ASSET_DIR/clicd" ] || die "下载的发行版包中未找到 clicd 二进制。"
}

install_binary() {
    if has_cmd systemctl; then
        systemctl stop clicd >/dev/null 2>&1 || true
    fi
    if has_cmd rc-service; then
        rc-service clicd stop >/dev/null 2>&1 || true
    fi

    bin_src="./clicd"
    download_dir=""
    if [ ! -f "$bin_src" ] && [ -f "$INSTALL_DOWNLOAD_MARKER" ]; then
        download_dir="$(sed -n '1p' "$INSTALL_DOWNLOAD_MARKER" 2>/dev/null || true)"
        if [ -n "$download_dir" ] && [ -f "$download_dir/$ASSET_DIR/clicd" ]; then
            bin_src="$download_dir/$ASSET_DIR/clicd"
        fi
    fi
    [ -f "$bin_src" ] || die "未找到 clicd 二进制，安装无法继续。"

    tmp_bin="/usr/local/bin/clicd.new.$$"
    cp "$bin_src" "$tmp_bin"
    chmod +x "$tmp_bin"
    mv -f "$tmp_bin" /usr/local/bin/clicd
    chmod +x /usr/local/bin/clicd
    log "已安装二进制：/usr/local/bin/clicd"

    if [ -n "$download_dir" ]; then
        case "$download_dir" in
            /tmp/*)
                rm -rf "$download_dir"
                ;;
        esac
        rm -f "$INSTALL_DOWNLOAD_MARKER"
    fi
}

install_systemd_service() {
    libvirt_after="$(systemd_existing_units libvirtd.service virtqemud.service virtqemud.socket virtlogd.socket)"
    libvirt_wants="$(systemd_existing_units libvirtd.service virtqemud.socket virtlogd.socket)"
    lxc_after="$(systemd_existing_units lxc.service lxcfs.service lxc-net.service)"

    cat > /etc/systemd/system/clicd.service << EOF
[Unit]
Description=CLICD - LXC/KVM Container Manager
After=network-online.target${lxc_after}${libvirt_after}
Wants=network-online.target${libvirt_wants}
StartLimitIntervalSec=60
StartLimitBurst=10

[Service]
Type=simple
ExecStart=/usr/local/bin/clicd server
Restart=always
RestartSec=5
LimitNOFILE=1048576
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable clicd
    systemctl restart clicd
}

install_openrc_service() {
    cat > /etc/init.d/clicd << 'EOF'
#!/sbin/openrc-run

name="CLICD"
description="CLICD - LXC/KVM Container Manager"
command="/usr/local/bin/clicd"
command_args="server"
command_background=true
pidfile="/run/clicd.pid"
output_log="/var/log/clicd.log"
error_log="/var/log/clicd.err"

depend() {
    need net
    after lxc libvirtd
}
EOF

    chmod +x /etc/init.d/clicd
    rc-update add clicd default
    rc-service clicd restart
}

install_service() {
    log "正在安装 CLICD 服务..."

    if is_systemd; then
        install_systemd_service
    elif is_openrc; then
        install_openrc_service
    else
        die "未检测到支持的服务管理器。CLICD 当前支持 systemd 或 OpenRC。"
    fi
}

set_panel_language() {
    lang="$CLICD_LANG_DETECTED"
    db="/root/.clicd/config.db"
    if [ "$lang" != "zh" ] && [ "$lang" != "en" ]; then
        lang="zh"
    fi

    saved=0
    i=0
    while [ ! -f "$db" ] && [ "$i" -lt 20 ]; do
        i=$((i + 1))
        sleep 1
    done

    if [ -f "$db" ] && has_cmd python3; then
        CLICD_PANEL_LANG="$lang" CLICD_DB="$db" python3 - <<'PY' >/dev/null 2>&1 && saved=1 || saved=0
import os
import sqlite3

db = os.environ["CLICD_DB"]
lang = os.environ["CLICD_PANEL_LANG"]
conn = sqlite3.connect(db)
conn.execute("CREATE TABLE IF NOT EXISTS app_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
conn.execute("INSERT OR REPLACE INTO app_meta(key, value) VALUES('language', ?)", (lang,))
conn.commit()
conn.close()
PY
    fi

    if [ "$saved" != "1" ] && [ -f "$db" ] && has_cmd sqlite3; then
        sqlite3 "$db" "CREATE TABLE IF NOT EXISTS app_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT OR REPLACE INTO app_meta(key, value) VALUES('language', '$lang');" >/dev/null 2>&1 && saved=1 || saved=0
    fi

    if [ "$saved" != "1" ] && has_cmd curl; then
        curl -k -fsS -X POST -H 'Content-Type: application/json' -d "{\"language\":\"$lang\"}" "https://127.0.0.1:8999/api/language" >/dev/null 2>&1 && saved=1 || \
        curl -fsS -X POST -H 'Content-Type: application/json' -d "{\"language\":\"$lang\"}" "http://127.0.0.1:8999/api/language" >/dev/null 2>&1 && saved=1 || saved=0
    fi

    if [ "$saved" = "1" ]; then
        log "已写入面板语言：$lang"
        if has_cmd systemctl && systemctl is-active clicd >/dev/null 2>&1; then
            systemctl restart clicd >/dev/null 2>&1 || true
        elif has_cmd rc-service; then
            rc-service clicd restart >/dev/null 2>&1 || true
        fi
    else
        warn "面板语言写入失败，请安装后在面板右下角手动切换。"
    fi
}

print_summary() {
    echo ""
    echo "====================================="
    echo "  $(tr_msg "安装完成")"
    echo "====================================="
    echo "  $(tr_msg "Web 面板：")http://YOUR_SERVER_IP:8999"
    echo "  $(tr_msg "二进制：")/usr/local/bin/clicd"
    echo "  $(tr_msg "安装日志：")$LOG_FILE"
    echo "  $(tr_msg "问题反馈：")$ISSUE_URL"
    if is_systemd; then
        echo "  $(tr_msg "服务：")systemctl {start|stop|restart|status} clicd"
        echo "  $(tr_msg "运行日志：")journalctl -u clicd -f"
    elif is_openrc; then
        echo "  $(tr_msg "服务：")rc-service clicd {start|stop|restart|status}"
        echo "  $(tr_msg "运行日志：")tail -f /var/log/clicd.log /var/log/clicd.err"
    fi
    echo "====================================="
    echo ""
    echo "$(tr_msg "首次安装时的初始账号信息：")"
    if is_systemd; then
        journalctl -u clicd --no-pager -n 80 | grep -E "Username:|Password:" || true
    else
        grep -E "Username:|Password:" /var/log/clicd.log /var/log/clicd.err 2>/dev/null || true
    fi
    echo ""
    echo "$(tr_msg "如果没有显示密码，说明服务器已有") /root/.clicd/config.db."
    echo "$(tr_msg "已有管理员密码使用 bcrypt 存储，无法反查；请使用面板内修改密码或重置配置。")"
}

run_step "兼容性检查" check_os_compatibility
run_step "存储环境检查" check_storage_compatibility
run_step "安装系统依赖" install_dependencies
run_step "配置内核网络参数" configure_kernel_networking
run_step "配置运行时服务" setup_runtime_services
run_step "配置 libvirt default NAT 网络" setup_default_libvirt_network
run_step "配置 UID/GID 映射" setup_subids
run_step "配置 LXC 存储权限" configure_lxc_storage_access
run_step "检查 project quota" try_enable_project_quota
run_step "下载发行版包" download_release_if_needed
run_step "安装 CLICD 二进制" install_binary
run_step "安装并启动 CLICD 服务" install_service
run_step "写入面板语言" set_panel_language
sleep 2
print_summary
