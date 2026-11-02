# 项目介绍

CLICD 是一个面向 LXC/KVM 的轻量虚拟化管理面板。它把常见宿主机运维动作收敛到 Web 控制台和命令行里，适合用来管理小型 VPS、独立服务器或需要批量分发容器访问权限的场景。

## 核心能力

- 管理 LXC 容器和 KVM 虚拟机。
- 创建、开机、关机、重启、重装、删除容器。
- 配置 CPU、内存、磁盘、流量限制和到期时间。
- 管理 NAT4 端口映射，并在宿主机具备 IPv6 路由时分配公网 IPv6。
- 在浏览器中打开 WebSSH 或 WebVNC。
- 管理镜像下载、启用状态和本地缓存。
- 创建、恢复、删除快照，配置计划快照和快照配额。
- 保留操作审计日志和登录日志。
- 为指定容器创建子用户访问链接。
- 通过 API Key 接入 `/api/v1` 自动化接口。

## 适用场景

- 一台宿主机上需要快速分配多个 Linux 容器。
- 需要给用户临时发放容器控制台、SSH、VNC 或 NAT 端口管理权限。
- 希望用 API 自动化创建容器、调整资源、重置密码或回收资源。
- 需要一个比纯 CLI 更直观，但又不重型的平台面板。

## 技术栈

- 后端：Go、`net/http`、SQLite、systemd、LXC、KVM/libvirt、cgroup v2、iptables、conntrack。
- 前端：React、TypeScript、Vite、Tailwind CSS、lucide-react、xterm.js、noVNC。
- 发布：GitHub Actions 构建 Linux AMD64/ARM64 release 产物，安装脚本默认拉取最新 Release。
