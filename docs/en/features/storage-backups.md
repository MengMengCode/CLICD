# Disks, Full Backups and Clones

Administrators manage disks and backups from instance details. Sub-users can read disk information for their assigned instances. Long operations return a `task_id` through the [task queue](./tasks); wait for completion before the next disk or lifecycle operation.

## Grow a System Disk

Shut down the instance first. The backend checks the actual LXC/libvirt state; running, paused and frozen instances cannot change disks.

| Runtime | Supported operation | After growing |
| --- | --- | --- |
| KVM | Offline growth of managed QCOW2 system/data disks; shrinking is rejected. | Extend partitions, LVM and filesystems inside the guest yourself. CLICD only enlarges the virtual disk. |
| LXC | Offline growth of managed ext4 loop images, including a filesystem check and `resize2fs`. | Start the container after success. Directory rootfs layouts, other filesystems and quota backends do not have a general resize implementation. |

Disk inventory reports the actual image capacity and allocated host space. These can differ for sparse files. Resource limits do not replace a real disk resize.

## Data Disks

Each instance supports up to 8 managed data disks. Select a name, capacity and optional storage pool. LXC also needs a guest mount path.

- KVM uses independent QCOW2 files. Partition, format and configure persistent mounts inside the guest.
- LXC uses independent ext4 images mounted outside rootfs and bind-mounted into the guest. Paths must be `/data`, `/mnt`, `/srv`, or their descendants. Paths cannot overlap, cross symlinks or hide an existing non-empty directory.
- Add, grow, attach, detach and delete require a stopped instance. Detach keeps the file. Delete requires explicit `delete_data: true` and permanently removes the managed data file.
- Power operations and system reinstallation preserve data disks. Full backups include every managed disk, including detached disks.

## Full Backups

A full backup contains the system disk, all data disks and necessary instance settings. Download it as `.clicd.tar.gz` and import it on a CLICD node with a compatible CPU architecture. KVM disks become independent QCOW2 files without the original backing image. LXC stores independent ext4 images.

Backup creation, full cloning and saving an instance template use cold copies. A running source is temporarily stopped, and CLICD attempts to restore its prior running state afterward. Allow for downtime and sufficient disk space.

Current portable format limits:

- The archive and its expanded disk-file payload each have a **64 GiB** limit. LXC raw images count their full logical capacity toward the payload. Sparse KVM QCOW2 virtual capacities can exceed 64 GiB, with a 16 TiB virtual-capacity limit per disk.
- Import/restore checks the format version, CPU architecture, complete disk inventory and every file's SHA-256. Archives missing a data disk are rejected.
- Compression does not bypass the expanded-payload limit. After shutdown, KVM estimates the flattened QCOW2 file size and checks temporary copy space. Creation, extraction and restore still need additional capacity.

Archives preserve guest accounts, passwords and disk contents, and can contain saved SSH login information. Handle downloaded archives as sensitive data.

## Restore, Full Clone and Instance Templates

All three operations create a new instance, leave it **stopped**, and preserve the source:

| Operation | Source |
| --- | --- |
| Restore backup | A locally created or uploaded full backup. |
| Full clone | An independent copy of the current instance's disks. |
| Instantiate template | A reusable full template saved from an installed instance. |

The new instance receives a new ID, UUID, MAC and host storage paths. Public IPv4/IPv6 assignments, port mappings, sub-user grants, firewall policies and snapshot schedules are not copied directly. New NAT management ports can be allocated. Linux guests also receive a new hostname, machine identity, SSH host keys and DHCP network configuration. Windows networking is adjusted to DHCP on first boot. **Guest accounts and login passwords are preserved**.

The instance-template page can create 1–10 copies per submission. Multiple copies use a name prefix and numbered suffixes, with one independent task per copy. Partial failures keep accepted tasks visible and identify each rejected name.

KVM restore, clone and template instantiation require the guestfs tools `virt-customize`, `virt-inspector`, `virt-cat`, `virt-edit` and `virt-filesystems` to update guest network identity offline. The tools must recognize a single installed operating system; encrypted disks, multiple operating systems and uninstalled blank disks return an explicit error. Windows first-boot networking additionally needs a guestfs-compatible `rhsrvany` or `pvvxsvc` helper. Missing dependencies prevent publication of the new instance. See [installation dependencies](../guide/installation#kvm-guestfs).

## Existing Snapshots

The existing snapshot workflow supports the system disk. If an instance has any data disks, creating/restoring old snapshots and enabling scheduled snapshots are rejected to prevent incomplete rollback. Detached disks count too. Disable an existing snapshot schedule before adding data disks.

Use full backups for rollback or migration with data disks. Snapshot quotas do not limit full backup files; remove old artifacts separately in backup management.

## API

See [disk and backup workflows](./api#disk-and-backup-workflows) for request bodies, scopes, task polling, archive downloads and imports.
