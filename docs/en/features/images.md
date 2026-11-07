# Image Management

Image Management maintains templates used to create containers or virtual machines.

The main page shows downloaded/downloading images; open the download dialog for the preset catalog. Upload local ISO/QCOW2 files as KVM images or maintain third-party download sources.

## Supported Template Types

The project includes common Linux distribution templates such as Debian, Ubuntu, Alpine, CentOS, Fedora, Arch Linux, and Rocky Linux. KVM templates use the corresponding distribution cloud image resources.

## Management Actions

See [local image upload and KVM installation media](./api#local-image-upload-and-kvm-installation-media) for the API request format and scopes.

## Local Images and Generic ISO Installation

Uploads validate file content, optional SHA-256 and disk structure. A filename extension alone is insufficient. QCOW2 files must be independent, without external backing/data files, encryption or internal snapshots. Images are registered only after the full upload and validation succeed.

`provisioner=manual-iso` supports manual Linux, Windows and other OS installation: create an empty system disk, attach the ISO and install through WebVNC. It does not require a Windows unattended image or wait for SSH. The KVM installation-media button in instance details opens a dialog to select a downloaded ISO, choose CD/disk boot or eject the media while the VM is stopped. Arbitrary host paths are not accepted. Media and ejection state persist across power operations and resource changes. An explicit reinstall prepares media from the newly selected template.

Preinstalled QCOW2 defaults to `manual-disk`, preserving its accounts and network settings. Linux images with cloud-init can explicitly use `linux-cloud-init`. Uploaded images must match the host CPU architecture. For installed-instance templates including data disks, use [full backups and clones](./storage-backups).

## Windows Images

This project does not distribute Windows system images and does not provide features to bypass or avoid Windows activation. Windows download links should point to official Microsoft resources, and users must obtain valid licenses themselves.
