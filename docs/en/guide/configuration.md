# Configuration

After installation, CLICD runs as a systemd service. Runtime configuration and the database are stored locally on the host. The exact path may vary with installer options, but the default installation should mainly be checked under `/root/.clicd/`.

## Common Settings

| Setting | Description |
| --- | --- |
| Web port | Defaults to `8999`, listening on `0.0.0.0:8999`. |
| Administrator account | Used to log in to the web panel and manage API keys. |
| Database | SQLite storage for container metadata, sub-users, audit logs, API keys, and more. |
| NAT port range | Used for random ports and port mapping allocation. |
| IPv6 prefixes | Used when the host has routable IPv6 prefixes. |
| Security alerts | Policies such as automatic shutdown can be configured. |

## Service Commands

```bash
systemctl status clicd
systemctl restart clicd
journalctl -u clicd -n 100 --no-pager
```

## Panel Access Allowlist CLI

```bash
# Show the current policy
clicd access-policy show

# Allow selected addresses and networks; add reverse proxies when needed
clicd access-policy set \
  --allow "203.0.113.10,192.168.1.0/24,2001:db8::/32" \
  --trusted-proxy "127.0.0.1"

# Disable source restrictions
clicd access-policy disable
```

The same controls are available from the "Panel access allowlist" item in `clicd cli`. Both paths persist the setting and restart the running panel service automatically.

## Security Recommendations

- Do not expose the web panel directly to untrusted networks.
- Use a strong administrator password and rotate it regularly.
- Split API keys by purpose and avoid long-lived full-access keys.
- WebSSH and WebVNC tickets are short-lived credentials and should not be written to logs or shared publicly.
- Do not paste real IPs, passwords, API keys, or tickets into public docs, screenshots, or support tickets.
