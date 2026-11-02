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

镜像源创建、下载、取消、删除和启用管理需要管理员权限。使用外部 API Key 时，需同时授予 `admin:access` 和对应的 `image:*` 操作权限。

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
