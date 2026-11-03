# 部署建议

CLICD 可以直接运行在宿主机上，也可以放在反向代理之后。生产环境建议先做好访问控制，再开放给管理员使用。

## 服务暴露

默认 Web 端口为 `8999`：

```text
http://YOUR_SERVER_IP:8999
```

建议：

- 仅允许固定管理员 IP 访问。
- 使用反向代理配置 HTTPS。
- 不要在公开文档或截图里暴露真实登录地址。

## systemd

常用命令：

```bash
systemctl status clicd
systemctl restart clicd
systemctl enable clicd
journalctl -u clicd -f
```

## 防火墙

至少确认：

- 面板端口只对可信来源开放。
- NAT 映射端口按需开放。
- SSH 管理端口不与容器映射冲突。
- IPv6 防火墙规则与 IPv4 同步规划。

## 备份

建议定期备份：

- CLICD 配置目录。
- SQLite 数据库。
- 容器配置。
- 关键容器的快照或外部数据备份。

面板支持[完整实例备份](../features/storage-backups)下载和导入，包含系统盘及全部受管数据盘。它不替代配置/数据库备份；任务与历史指标仍保存在面板 SQLite 数据库中。带数据盘实例应使用完整备份，不能依赖旧系统盘快照。
