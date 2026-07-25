# 安装

CLICD 提供一键安装脚本。脚本默认安装 GitHub Releases 的最新版本，也可以通过环境变量指定固定版本。

## 环境要求

- Linux x86_64/amd64 或 ARM64/aarch64 宿主机。
- root 权限。
- systemd。
- 网络可访问 GitHub Release 下载地址。
- 如果要使用 LXC，需要宿主机支持 LXC 运行环境。
- 如果要使用 KVM，需要宿主机开启虚拟化并安装 libvirt/QEMU。

## 安装最新版本

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh
```

安装器会分别询问 LXC 与 KVM 的 NAT 私网网段。直接回车时，脚本会扫描宿主机路由、网卡、网桥和 libvirt 网络，自动选择未冲突的 RFC1918 `/24` 网段；也可以输入 `172.28.40.0/24` 这类 CIDR。非交互安装可设置 `CLICD_LXC_SUBNET` 和 `CLICD_KVM_SUBNET`。

脚本当前默认使用 `CLICD_VERSION=latest`，会按宿主架构下载 `releases/latest` 对应的 `clicd-linux-amd64.tar.gz` 或 `clicd-linux-arm64.tar.gz`。

## 安装指定版本

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo CLICD_VERSION=v1.1.6 sh
```

把 `v1.1.6` 替换成需要安装的 Release 标签即可。

## 访问面板

安装完成后，浏览器访问：

```text
http://YOUR_SERVER_IP:8999
```

首次登录请使用安装脚本输出的管理员账号信息。生产环境建议在防火墙或反向代理层限制访问来源，并尽快修改默认账号和密码。

## 卸载

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh -s -- uninstall
```

卸载前请确认是否需要保留容器、镜像缓存、数据库和配置文件。
