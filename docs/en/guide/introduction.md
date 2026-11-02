# Introduction

CLICD is a lightweight virtualization management panel for LXC and KVM. It brings common host operations into a web console and CLI, making it suitable for small VPS nodes, dedicated servers, and scenarios where container access needs to be distributed in batches.

## Core Capabilities

- Manage LXC containers and KVM virtual machines.
- Create, start, stop, restart, reinstall, and delete containers.
- Configure CPU, memory, disk, traffic limits, and expiration time.
- Manage NAT4 port mappings, public IPv4 assignment, and public IPv6 assignment when the host network supports it.
- Open WebSSH or WebVNC from the browser.
- Manage image downloads, enablement, and local cache.
- Create, restore, and delete snapshots, plus scheduled snapshots and quotas.
- Keep operation audit logs and login logs.
- Create sub-user access links for specific containers.
- Integrate automation through API keys and `/api/v1`.

## Use Cases

- Quickly allocate multiple Linux containers on one host.
- Give users temporary access to a container console, SSH, VNC, or NAT port management.
- Automate container creation, resource changes, password resets, or resource cleanup through the API.
- Use a panel that is clearer than pure CLI workflows without becoming a heavy platform.

## Tech Stack

- Backend: Go, `net/http`, SQLite, systemd, LXC, KVM/libvirt, cgroup v2, iptables, conntrack.
- Frontend: React, TypeScript, Vite, Tailwind CSS, lucide-react, xterm.js, noVNC.
- Release: GitHub Actions builds Linux AMD64/ARM64 release artifacts. The installer fetches the latest release by default.
