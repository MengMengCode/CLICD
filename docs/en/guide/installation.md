# Installation

CLICD provides a one-line installer. By default, it installs the latest version from GitHub Releases. You can also pin a specific version with an environment variable.

## Requirements

- Linux x86_64/amd64 or ARM64/aarch64 host.
- Root privileges.
- systemd.
- Network access to GitHub Release downloads.
- LXC runtime support if you want to use LXC.
- KVM virtualization enabled with libvirt/QEMU installed if you want to use KVM.

## Install the Latest Version

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh
```

The installer asks for separate LXC and KVM NAT private subnets. Press Enter to scan host routes, interfaces, bridges, and libvirt networks and select non-overlapping RFC1918 `/24` networks, or enter a CIDR such as `172.28.40.0/24`. For unattended installation, set `CLICD_LXC_SUBNET` and `CLICD_KVM_SUBNET`.

The script defaults to `CLICD_VERSION=latest` and downloads `clicd-linux-amd64.tar.gz` or `clicd-linux-arm64.tar.gz` from `releases/latest` according to the host architecture.

## Install a Specific Version

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo CLICD_VERSION=v1.1.6 sh
```

Replace `v1.1.6` with the release tag you want to install.

## Open the Panel

After installation, open:

```text
http://YOUR_SERVER_IP:8999
```

Use the administrator credentials printed by the installer for the first login. In production, restrict access at the firewall or reverse proxy layer and change the default username and password as soon as possible.

## Optional KVM Restore and Clone Dependencies {#kvm-guestfs}

KVM full restore, full clone and instance-template instantiation use `virt-customize` to update guest network identity offline. On supported KVM architectures, the installer tries these packages in order and checks whether the command is actually available:

| Distribution | Candidate packages |
| --- | --- |
| Debian / Ubuntu | `guestfs-tools`, falling back to `libguestfs-tools`. |
| RHEL / Rocky / AlmaLinux / Fedora | `libguestfs-tools`, then `guestfs-tools` and `libguestfs-tools-c`. |
| Alpine | Try `guestfs-tools` and `libguestfs-tools`; show a clear notice if unavailable for the repository/architecture. |

A missing dependency does not block basic LXC/KVM installation. The affected KVM restore/clone tasks fail explicitly instead of skipping identity updates. See the Ubuntu [guestfs-tools file list](https://packages.ubuntu.com/en/jammy/arm64/guestfs-tools/filelist) and the [Red Hat package documentation](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/7/html/virtualization_deployment_and_administration_guide/sect-guest_virtual_machine_disk_access_with_offline_tools-using_virt_customize).

```bash
command -v virt-customize
virt-customize --version
```

Growing managed LXC ext4 loop images also requires `e2fsck`, `resize2fs`, `losetup` and mount utilities. The installer retains `e2fsprogs` and related dependencies. Keep `conntrack`, which is still used for NAT connection cleanup.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh -s -- uninstall
```

Before uninstalling, decide whether you need to keep containers, image cache, database files, or configuration files.
