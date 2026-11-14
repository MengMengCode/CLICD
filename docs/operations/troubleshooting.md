# 故障排查

## 服务无法访问

检查服务状态：

```bash
systemctl status clicd
journalctl -u clicd -n 100 --no-pager
```

检查端口监听：

```bash
ss -lntp | grep 8999
```

如果使用反向代理，请同时检查代理日志和上游地址。

## 镜像下载失败

- 确认宿主机可以访问镜像源和 GitHub Release。
- 检查磁盘空间。
- 在任务队列里查看失败原因。
- 如下载卡住，可尝试取消任务后重新下载。

## Debian 12 等 LXC 容器启动后立即退出

如果启动日志包含 `Failed to create control group inotify object` 和 `Too many open files`，请先在宿主机查看 inotify 实例上限：

```bash
sysctl fs.inotify.max_user_instances
```

多个非特权 LXC 容器可能使用相同的宿主机 UID 映射，并共同消耗 inotify 实例配额。配额耗尽时，systemd 可能在初始化阶段退出，即使容器内的 `nofile` 上限足够高。

主机报告中的 `fs.inotify.max_user_instances` 检查会显示当前上限和建议下限。每次通过 CLICD 启动 LXC 容器时，如果运行中的宿主机上限低于 2048，CLICD 会尝试把它提高到 2048。如果 2048 仍不足，可以排查持有大量 inotify 实例的进程，或按负载在宿主机设置更高的上限。例如，可将以下内容写入 `/etc/sysctl.d/99-local-lxc-inotify.conf`，数值应高于当前上限：

```ini
fs.inotify.max_user_instances = 4096
```

在宿主机执行 `sudo sysctl -p /etc/sysctl.d/99-local-lxc-inotify.conf`，确认新值生效后再启动容器。配置文件会在宿主机下次启动时重新加载。

## 容器无法联网

- 检查宿主机 NAT 和转发规则。
- 检查容器 IP 是否分配成功。
- 检查防火墙是否拦截转发流量。
- IPv6 场景下确认上游已经把地址段路由到宿主机。

## WebSSH 或 WebVNC 连接失败

- 确认容器或虚拟机正在运行。
- WebSSH 需要容器内 SSH 服务可用。
- WebVNC 需要 KVM 控制台可访问。
- 票据有效期很短，过期后重新创建即可。

## API 返回未授权

- 确认 API Key 没有被禁用。
- 确认请求头使用 `X-API-Key` 或 `Authorization: Bearer`。
- 确认密钥权限范围覆盖目标接口。
- 不要把面板登录密码当作 API Key 使用。
