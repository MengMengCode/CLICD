# 镜像管理

镜像管理用于维护可创建容器或虚拟机的模板。

## 支持的模板类型

项目内置了常见 Linux 发行版模板，例如 Debian、Ubuntu、Alpine、CentOS、Fedora、Arch Linux、Rocky Linux 等。KVM 模板会使用对应发行版的云镜像资源。

## 管理动作

```http
GET /api/v1/templates
GET /api/v1/images
POST /api/v1/images/download
POST /api/v1/images/cancel
DELETE /api/v1/images/delete
PUT /api/v1/images/toggle
```

- `templates` 返回可用模板定义。
- `images` 返回本地镜像状态。
- `download` 下载指定模板。
- `cancel` 取消下载任务。
- `delete` 删除本地镜像缓存。
- `toggle` 控制模板是否对创建流程可用。

## Windows 镜像说明

- ISO 镜像用于安装 Windows，可通过 WebVNC 查看安装过程。
- QCOW2 镜像作为已有系统盘使用，保留镜像内的登录凭据和网络设置，不会自动注入新的随机密码。请使用镜像原有凭据，并按实际网络调整客户机设置。
- 当前 Windows 虚拟机仅支持传统 BIOS/MBR 启动，尚不支持 UEFI。使用自定义 QCOW2 前，请确认镜像的启动方式与之匹配。

本项目不分发 Windows 系统镜像，也不提供绕过或规避 Windows 激活机制的功能。涉及 Windows 的下载链接应指向微软官方资源，使用者需要自行获得合法授权。
