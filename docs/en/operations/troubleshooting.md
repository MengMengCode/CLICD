# Troubleshooting

## Service Not Reachable

Check service status:

```bash
systemctl status clicd
journalctl -u clicd -n 100 --no-pager
```

Check port listening:

```bash
ss -lntp | grep 8999
```

If a reverse proxy is used, check proxy logs and upstream address settings as well.

## Image Download Failed

- Make sure the host can access image sources and GitHub Releases.
- Check disk space.
- Review the failure reason in the task queue.
- If a download is stuck, cancel it and start again.

## Debian 12 and Other LXC Containers Exit on Startup

If the startup log contains `Failed to create control group inotify object` and `Too many open files`, check the inotify instance limit on the host:

```bash
sysctl fs.inotify.max_user_instances
```

Unprivileged LXC containers can share the same host UID mapping, so processes in several containers can draw from the same inotify instance quota. If they exhaust it, systemd can exit during initialization even when the container's `nofile` limit is high enough.

The `fs.inotify.max_user_instances` check in the Host Report shows the current limit and the recommended minimum. When CLICD starts an LXC container, it tries to raise the host's running limit to 2048 if the current limit is lower. If 2048 is still insufficient, check for processes holding many inotify instances or configure a higher limit for the host's workload. For example, add the following to `/etc/sysctl.d/99-local-lxc-inotify.conf`, using a value above the current limit:

```ini
fs.inotify.max_user_instances = 4096
```

Run `sudo sysctl -p /etc/sysctl.d/99-local-lxc-inotify.conf` on the host, confirm that the new value is active, then start the container again. The configuration file is loaded on the next host boot.

## Container Cannot Access the Network

- Check host NAT and forwarding rules.
- Confirm that the container IP was assigned successfully.
- Check whether the firewall is blocking forwarded traffic.
- For IPv6, confirm that the upstream network routes the prefix to the host.

## WebSSH or WebVNC Connection Failed

- Confirm that the container or virtual machine is running.
- WebSSH requires SSH service inside the container.
- WebVNC requires the KVM console to be reachable.
- Tickets expire quickly. Create a new ticket after expiration.

## API Returns Unauthorized

- Confirm that the API key is not disabled.
- Use `X-API-Key` or `Authorization: Bearer`.
- Confirm that the key scope covers the target endpoint.
- Do not use the panel login password as an API key.
