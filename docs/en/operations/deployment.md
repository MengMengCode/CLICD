# Deployment

CLICD can run directly on the host or behind a reverse proxy. In production, set up access control before exposing it to administrators.

## Service Exposure

The default web port is `8999`:

```text
http://YOUR_SERVER_IP:8999
```

Recommendations:

- Allow only fixed administrator IPs.
- Use a reverse proxy with HTTPS.
- Do not expose the real login URL in public docs or screenshots.

## systemd

Common commands:

```bash
systemctl status clicd
systemctl restart clicd
systemctl enable clicd
journalctl -u clicd -f
```

## Firewall

At minimum, confirm:

- The panel port is open only to trusted sources.
- NAT mapped ports are opened only as needed.
- The SSH management port does not conflict with container mappings.
- IPv6 firewall rules are planned together with IPv4 rules.

## Backups

Back up regularly:

- CLICD configuration directory.
- SQLite database.
- Container configuration.
- Snapshots or external data backups for important containers.

[Full instance backups](../features/storage-backups) include the system disk and all managed data disks and support download/import. They do not replace configuration/database backups; task and metric history remains in the panel SQLite database. Use full backups for instances with data disks instead of existing system-disk snapshots.
