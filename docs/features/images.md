# 镜像管理

镜像管理用于维护可创建容器或虚拟机的模板。

主页面显示本地已下载和正在下载的镜像；预设目录从下载按钮打开。可将本地 ISO/QCOW2 文件上传为 KVM 镜像，也可维护第三方下载源。

## 支持的模板类型

项目内置了常见 Linux 发行版模板，例如 Debian、Ubuntu、Alpine、CentOS、Fedora、Arch Linux、Rocky Linux 等。KVM 模板会使用对应发行版的云镜像资源。

## 管理动作

接口权限、上传字段和示例见 [API 集成](./api#本地镜像上传和-kvm-安装介质)。

## 本地镜像与通用 ISO

上传会检查文件内容、可选 SHA-256 及磁盘格式，扩展名本身不能证明文件有效。QCOW2 必须是独立磁盘，不能依赖外部 backing/data file，也不能带加密或内嵌快照。只会在上传和验证全部成功后注册镜像。

`provisioner=manual-iso` 用于 Linux、Windows 或其他系统的手动安装：创建空白系统盘并挂载 ISO，通过 WebVNC 安装，不强制生成 Windows 无人值守盘或等待 SSH。详情页顶部的“KVM 安装介质”按钮打开介质弹窗，支持停止状态下选择已下载 ISO、设置从光盘/磁盘启动或弹出 ISO；界面不接受任意宿主机路径。介质选择及弹出状态持久化，开关机和修改资源不会重新挂回已弹出的 ISO；主动重装则按新选模板重新准备安装介质。

预装 QCOW2 默认使用 `manual-disk`，保留镜像内已有账户和网络设置；具备 cloud-init 的 Linux 镜像可明确选择 `linux-cloud-init`。上传的架构必须与宿主机匹配。完整实例模板（含数据盘）使用[完整备份与克隆](./storage-backups)的模板工作流。

## 私网镜像源

下载器默认只连接公网镜像。要使用内网镜像站，请在服务器上设置 `CLICD_IMAGE_MIRROR_ORIGINS`，列出允许访问的来源，多个来源用逗号分隔：

```ini
[Service]
Environment="CLICD_IMAGE_MIRROR_ORIGINS=http://192.168.1.10:8080,https://mirror.example.lan"
```

systemd 部署可通过 `sudo systemctl edit clicd` 添加上述配置，再执行 `sudo systemctl daemon-reload` 和 `sudo systemctl restart clicd`。

每项只填写协议、主机和可选端口，不填写镜像路径、查询参数或登录凭据。镜像的完整下载 URL 仍在面板中填写。协议或端口不同的来源需要单独批准；重定向不会扩大内网访问范围。本机回环、链路本地和云元数据地址始终禁止访问。

## Windows 镜像说明

- ISO 镜像用于安装 Windows，可通过 WebVNC 查看安装过程。
- QCOW2 镜像作为已有系统盘使用，保留镜像内的登录凭据和网络设置，不会自动注入新的随机密码。请使用镜像原有凭据，并按实际网络调整客户机设置。
- 当前 Windows 虚拟机仅支持传统 BIOS/MBR 启动，尚不支持 UEFI。使用自定义 QCOW2 前，请确认镜像的启动方式与之匹配。

本项目不分发 Windows 系统镜像，也不提供绕过或规避 Windows 激活机制的功能。涉及 Windows 的下载链接应指向微软官方资源，使用者需要自行获得合法授权。
