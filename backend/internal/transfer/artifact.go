// Package transfer stores portable, versioned instance backups. The outer
// archive contains only regular disk files; guest filesystem links/devices
// remain inside the filesystem image and are never extracted onto the host.
package transfer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
	"clicd/internal/version"
)

const FormatVersion = 1
const MaxExpandedBytes int64 = 64 << 30
const MaxArchiveBytes int64 = 64 << 30
const MaxVirtualDiskBytes int64 = 16384 << 30
const maxManifestBytes int64 = 4 << 20

var idPattern = regexp.MustCompile(`^(bk|tpl)-[a-f0-9]{32}$`)
var payloadPattern = regexp.MustCompile(`^disks/[a-z0-9-]+\.(qcow2|img)$`)
var mutationMu sync.Mutex
var selectBackupRoot = selectRoot
var allocateIdentityID = config.AllocateContainerID

var checkSpace = func(directory string, required int64) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	out, err := exec.Command("df", "-B1", "--output=avail", directory).Output()
	if err != nil {
		return fmt.Errorf("cannot check backup free space: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return fmt.Errorf("invalid backup free-space response")
	}
	available, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil {
		return err
	}
	if available < required+(256<<20) {
		return fmt.Errorf("backup storage has insufficient space (256 MiB reserve required)")
	}
	return nil
}

// CreationMu protects the final name check, ID/network reservation and commit
// of all transfer-created instances. It is never acquired under a queue mutex.
var CreationMu sync.Mutex

type File struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	DiskID string `json:"disk_id,omitempty"`
	Format string `json:"format"`
	Size   int64  `json:"size_bytes"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version     int              `json:"format_version"`
	AppVersion  string           `json:"app_version"`
	Runtime     string           `json:"runtime"`
	Arch        string           `json:"arch"`
	BootProfile string           `json:"boot_profile"`
	CreatedAt   string           `json:"created_at"`
	Container   config.Container `json:"container"`
	Files       []File           `json:"files"`
	RootUIDBase uint32           `json:"root_uid_base,omitempty"`
	RootGIDBase uint32           `json:"root_gid_base,omitempty"`
}

type Info struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Runtime              string `json:"runtime"`
	Arch                 string `json:"arch"`
	CreatedAt            string `json:"created_at"`
	SourceName           string `json:"source_name"`
	SizeBytes            int64  `json:"size_bytes"`
	DiskCount            int    `json:"disk_count"`
	FormatVersion        int    `json:"format_version"`
	SHA256               string `json:"sha256"`
	CredentialsPreserved bool   `json:"credentials_preserved"`
	ColdCopy             bool   `json:"cold_copy"`
}

type record struct {
	Info
	Manifest Manifest `json:"manifest"`
}
type Exporter func(context.Context, string, *Manifest) error
type Validator func(context.Context, string, *Manifest) error

func NewID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(bytes[:])
}

func ValidateManifest(m *Manifest) error {
	if m.Version != FormatVersion {
		return fmt.Errorf("unsupported backup format version")
	}
	if m.Arch != runtime.GOARCH {
		return fmt.Errorf("backup architecture %q does not match %s", m.Arch, runtime.GOARCH)
	}
	if m.Runtime != "kvm" && m.Runtime != "lxc" {
		return fmt.Errorf("invalid backup runtime")
	}
	maxDiskGB := float64(64)
	if m.Runtime == "kvm" {
		maxDiskGB = 16384
	}
	if m.Container.Runtime() != m.Runtime || m.Container.VCPU <= 0 || m.Container.VCPU > 1024 || m.Container.RAMMB < 128 || m.Container.RAMMB > 1048576 || m.Container.DiskGB <= 0 || m.Container.DiskGB > maxDiskGB || len(m.Container.DataDisks) > config.MaxDataDisks {
		return fmt.Errorf("invalid instance resources")
	}
	if m.BootProfile != "linux" && m.BootProfile != "windows" && m.BootProfile != "manual" {
		return fmt.Errorf("invalid boot profile")
	}
	if m.RootUIDBase > 0x7fff0000 || m.RootGIDBase > 0x7fff0000 {
		return fmt.Errorf("invalid subordinate ID range")
	}
	if m.Runtime == "kvm" && (m.Container.VCPU < 1 || m.Container.VCPU != float64(int(m.Container.VCPU))) {
		return fmt.Errorf("KVM backup requires integral vCPU count")
	}
	if len(m.Files) == 0 || len(m.Files) > 65 {
		return fmt.Errorf("invalid disk inventory")
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	var total int64
	systems := 0
	for _, f := range m.Files {
		if !payloadPattern.MatchString(f.Name) || seen[f.Name] || f.Size <= 0 || f.Size > MaxExpandedBytes || len(f.SHA256) != 64 {
			return fmt.Errorf("invalid backup file entry")
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return fmt.Errorf("invalid file checksum")
		}
		seen[f.Name] = true
		total += f.Size
		if total > MaxExpandedBytes {
			return fmt.Errorf("backup expands beyond 64 GiB limit")
		}
		if m.Runtime == "kvm" && f.Format != "qcow2" || m.Runtime == "lxc" && f.Format != "raw" {
			return fmt.Errorf("invalid disk format")
		}
		if f.Role == "system" {
			systems++
		} else if f.Role == "data" && f.DiskID != "" && !ids[f.DiskID] {
			ids[f.DiskID] = true
		} else {
			return fmt.Errorf("invalid disk role")
		}
	}
	if systems != 1 || len(ids) != len(m.Container.DataDisks) {
		return fmt.Errorf("backup does not contain every instance disk")
	}
	for _, d := range m.Container.DataDisks {
		if !ids[d.ID] {
			return fmt.Errorf("missing data disk")
		}
		if _, err := config.DiskSizeBytes(d.SizeGB); err != nil {
			return err
		}
		if m.Runtime == "lxc" {
			if err := config.ValidateGuestMountPath(d.MountPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func roots() []string {
	var result []string
	if config.AppConfig == nil {
		return result
	}
	for _, p := range config.AppConfig.StoragePools {
		for _, content := range p.ContentTypes {
			if content == config.StorageContentBackups {
				result = append(result, filepath.Join(p.Path, "backups", "instances"))
				break
			}
		}
	}
	return result
}

func selectRoot(poolID string, required int64) (string, error) {
	pool, err := config.SelectStoragePoolForContent(config.StorageContentBackups, poolID, required)
	if err != nil {
		return "", err
	}
	if poolID != "" && pool.ID != poolID {
		return "", fmt.Errorf("requested backup storage pool is unavailable or lacks capacity")
	}
	root := filepath.Join(pool.Path, "backups", "instances")
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func List(kind string) []Info {
	result := []Info{}
	for _, root := range roots() {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") || entry.IsDir() {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !idPattern.MatchString(id) {
				continue
			}
			r, _, err := loadAt(root, id)
			if err == nil && (kind == "" || r.Kind == kind) {
				result = append(result, r.Info)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result
}

func loadAt(root, id string) (*record, string, error) {
	archive := filepath.Join(root, id+".clicd.tar.gz")
	for _, p := range []string{filepath.Join(root, id+".json"), archive} {
		st, e := os.Lstat(p)
		if e != nil {
			return nil, "", e
		}
		if !st.Mode().IsRegular() {
			return nil, "", fmt.Errorf("artifact is not a regular file")
		}
	}
	data, err := os.ReadFile(filepath.Join(root, id+".json"))
	if err != nil {
		return nil, "", err
	}
	if len(data) > int(maxManifestBytes) {
		return nil, "", fmt.Errorf("artifact metadata too large")
	}
	var r record
	if err = json.Unmarshal(data, &r); err != nil {
		return nil, "", err
	}
	if r.ID != id || r.FormatVersion != FormatVersion {
		return nil, "", fmt.Errorf("invalid artifact record")
	}
	return &r, archive, nil
}

func Resolve(id string) (Info, string, error) {
	if !idPattern.MatchString(id) {
		return Info{}, "", fmt.Errorf("invalid artifact ID")
	}
	for _, root := range roots() {
		r, p, e := loadAt(root, id)
		if e == nil {
			return r.Info, p, nil
		}
	}
	return Info{}, "", os.ErrNotExist
}

func Delete(id, kind string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	info, p, err := Resolve(id)
	if err != nil {
		return err
	}
	if info.Kind != kind {
		return os.ErrNotExist
	}
	if err = os.Remove(p); err != nil {
		return err
	}
	return os.Remove(filepath.Join(filepath.Dir(p), id+".json"))
}

func Create(ctx context.Context, poolID, name, kind string, c config.Container, export Exporter) (Info, error) {
	if kind != "backup" && kind != "template" {
		return Info{}, fmt.Errorf("invalid artifact kind")
	}
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 {
		return Info{}, fmt.Errorf("artifact name must contain 1-128 characters")
	}
	required := int64(0)
	if !c.IsKVM() {
		required = int64(c.DiskGB * (1 << 30))
		for _, d := range c.DataDisks {
			required += int64(d.SizeGB * (1 << 30))
		}
		if required <= 0 || required > MaxExpandedBytes {
			return Info{}, fmt.Errorf("LXC raw filesystem payload exceeds 64 GiB backup limit")
		}
	}
	// QCOW2 logical capacity is deliberately not an archive byte count. Its
	// exporter measures the flattened files after the guest is safely stopped.
	root, err := selectBackupRoot(poolID, required)
	if err != nil {
		return Info{}, err
	}
	stage, err := os.MkdirTemp(root, ".building-")
	if err != nil {
		return Info{}, err
	}
	defer os.RemoveAll(stage)
	// Container contains slices. Sanitizing portable metadata must never mutate
	// the live source's DataDisks or other backing arrays.
	encoded, err := json.Marshal(c)
	if err != nil {
		return Info{}, err
	}
	var copied config.Container
	if err = json.Unmarshal(encoded, &copied); err != nil {
		return Info{}, err
	}
	m := Manifest{Version: FormatVersion, AppVersion: version.Current(), Runtime: c.Runtime(), Arch: runtime.GOARCH, CreatedAt: time.Now().UTC().Format(time.RFC3339), Container: copied, BootProfile: "linux"}
	if err = export(ctx, stage, &m); err != nil {
		return Info{}, err
	}
	for i := range m.Files {
		p := filepath.Join(stage, filepath.FromSlash(m.Files[i].Name))
		st, e := os.Lstat(p)
		if e != nil {
			return Info{}, e
		}
		if !st.Mode().IsRegular() {
			return Info{}, fmt.Errorf("payload is not a regular disk file")
		}
		m.Files[i].Size = st.Size()
		m.Files[i].SHA256, err = HashFile(ctx, p)
		if err != nil {
			return Info{}, err
		}
	}
	// Host paths are neither needed for restore nor disclosed in the portable metadata.
	m.Container.DiskImage = ""
	m.Container.StoragePath = ""
	m.Container.StoragePoolID = ""
	for i := range m.Container.DataDisks {
		m.Container.DataDisks[i].Path = ""
		m.Container.DataDisks[i].StoragePoolID = ""
	}
	if err = ValidateManifest(&m); err != nil {
		return Info{}, err
	}
	return pack(ctx, root, name, kind, stage, m)
}

func pack(ctx context.Context, root, name, kind, stage string, m Manifest) (Info, error) {
	id := NewID(map[string]string{"backup": "bk", "template": "tpl"}[kind])
	temp, err := os.CreateTemp(root, ".archive-")
	if err != nil {
		return Info{}, err
	}
	tmp := temp.Name()
	defer os.Remove(tmp)
	compressed, _ := gzip.NewWriterLevel(temp, gzip.BestSpeed)
	writer := tar.NewWriter(compressed)
	data, err := json.Marshal(m)
	if err != nil {
		temp.Close()
		return Info{}, err
	}
	if err = writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err == nil {
		_, err = writer.Write(data)
	}
	for _, file := range m.Files {
		if err != nil {
			break
		}
		err = writer.WriteHeader(&tar.Header{Name: file.Name, Mode: 0600, Size: file.Size, Typeflag: tar.TypeReg})
		if err != nil {
			break
		}
		source, e := os.Open(filepath.Join(stage, filepath.FromSlash(file.Name)))
		if e != nil {
			err = e
			break
		}
		_, err = Copy(ctx, writer, source)
		source.Close()
	}
	err = errors.Join(err, writer.Close(), compressed.Close(), temp.Sync(), temp.Close())
	if err != nil {
		return Info{}, err
	}
	archive := filepath.Join(root, id+".clicd.tar.gz")
	if err = os.Rename(tmp, archive); err != nil {
		return Info{}, err
	}
	info, err := saveRecord(ctx, archive, id, name, kind, m)
	if err != nil {
		os.Remove(archive)
	}
	return info, err
}

func saveRecord(ctx context.Context, archive, id, name, kind string, m Manifest) (Info, error) {
	st, err := os.Stat(archive)
	if err != nil {
		return Info{}, err
	}
	digest, err := HashFile(ctx, archive)
	if err != nil {
		return Info{}, err
	}
	info := Info{ID: id, Name: name, Kind: kind, Runtime: m.Runtime, Arch: m.Arch, CreatedAt: time.Now().UTC().Format(time.RFC3339), SourceName: m.Container.Name, SizeBytes: st.Size(), DiskCount: len(m.Files), FormatVersion: FormatVersion, SHA256: digest, CredentialsPreserved: true, ColdCopy: true}
	bytes, err := json.Marshal(record{Info: info, Manifest: m})
	if err != nil {
		return Info{}, err
	}
	p := filepath.Join(filepath.Dir(archive), id+".json")
	if err = os.WriteFile(p+".tmp", bytes, 0600); err != nil {
		return Info{}, err
	}
	err = os.Rename(p+".tmp", p)
	return info, err
}

// Extract validates every byte against a strict regular-file allowlist. There
// are no tar symlink/hardlink/device entries, duplicate names, absolute paths,
// parent traversal, optional arbitrary payloads or unbounded decompression.
func Extract(ctx context.Context, reader io.Reader, destination string) (*Manifest, error) {
	compressed, err := gzip.NewReader(io.LimitReader(reader, MaxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	limited := &io.LimitedReader{R: compressed, N: MaxExpandedBytes + maxManifestBytes + (1 << 20) + 1}
	archive := tar.NewReader(limited)
	var m *Manifest
	seen := map[string]bool{}
	var total int64
	count := 0
	for {
		h, e := archive.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		count++
		if count > 66 {
			return nil, fmt.Errorf("too many archive entries")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA || h.Linkname != "" || h.Size < 0 || h.Size > MaxExpandedBytes || h.Name != path.Clean(h.Name) || strings.Contains(h.Name, "\\") || strings.Contains(h.Name, ":") || path.IsAbs(h.Name) || seen[h.Name] {
			return nil, fmt.Errorf("unsafe archive entry")
		}
		seen[h.Name] = true
		if h.Name == "manifest.json" {
			if m != nil || h.Size > maxManifestBytes {
				return nil, fmt.Errorf("invalid manifest")
			}
			data, e := io.ReadAll(io.LimitReader(archive, maxManifestBytes+1))
			if e != nil {
				return nil, e
			}
			var next Manifest
			if e = json.Unmarshal(data, &next); e != nil {
				return nil, e
			}
			if e = ValidateManifest(&next); e != nil {
				return nil, e
			}
			m = &next
			var required int64
			for _, file := range m.Files {
				required += file.Size
			}
			if e = checkSpace(destination, required); e != nil {
				return nil, e
			}
			continue
		}
		if m == nil {
			return nil, fmt.Errorf("manifest must be the first archive entry")
		}
		var expected *File
		for i := range m.Files {
			if m.Files[i].Name == h.Name {
				expected = &m.Files[i]
				break
			}
		}
		if expected == nil || !payloadPattern.MatchString(h.Name) || h.Size != expected.Size {
			return nil, fmt.Errorf("unexpected disk payload")
		}
		total += h.Size
		if total > MaxExpandedBytes {
			return nil, fmt.Errorf("archive expansion limit exceeded")
		}
		p := filepath.Join(destination, filepath.FromSlash(h.Name))
		if st, e := os.Lstat(filepath.Join(destination, "disks")); e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("payload directory cannot be a symlink")
		}
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return nil, e
		}
		file, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		digest := sha256.New()
		n, copyErr := Copy(ctx, io.MultiWriter(file, digest), archive)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return nil, errors.Join(copyErr, closeErr)
		}
		if n != h.Size || hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
			return nil, fmt.Errorf("disk checksum or length mismatch")
		}
	}
	if m == nil || len(seen) != len(m.Files)+1 {
		return nil, fmt.Errorf("incomplete backup archive")
	}
	if limited.N <= 0 {
		return nil, fmt.Errorf("archive expansion limit exceeded")
	}
	var tail [1]byte
	if n, e := compressed.Read(tail[:]); n != 0 || e != io.EOF {
		return nil, fmt.Errorf("unexpected trailing archive data")
	}
	return m, nil
}

func Import(ctx context.Context, poolID, name string, reader io.Reader, validate Validator) (Info, error) {
	root, err := selectBackupRoot(poolID, 0)
	if err != nil {
		return Info{}, err
	}
	stage, err := os.MkdirTemp(root, ".import-")
	if err != nil {
		return Info{}, err
	}
	defer os.RemoveAll(stage)
	spool, err := os.CreateTemp(root, ".upload-")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(spool.Name())
	n, err := Copy(ctx, &spaceWriter{File: spool, Directory: root}, io.LimitReader(reader, MaxArchiveBytes+1))
	if err != nil {
		spool.Close()
		return Info{}, err
	}
	if n > MaxArchiveBytes {
		spool.Close()
		return Info{}, fmt.Errorf("upload exceeds 64 GiB limit")
	}
	if _, err = spool.Seek(0, 0); err != nil {
		spool.Close()
		return Info{}, err
	}
	m, err := Extract(ctx, spool, stage)
	spool.Close()
	if err != nil {
		return Info{}, err
	}
	if validate != nil {
		if err = validate(ctx, stage, m); err != nil {
			return Info{}, err
		}
	}
	if strings.TrimSpace(name) == "" {
		name = m.Container.Name
	}
	if utf8.RuneCountInString(name) > 128 {
		return Info{}, fmt.Errorf("invalid artifact name")
	}
	id := NewID("bk")
	archive := filepath.Join(root, id+".clicd.tar.gz")
	if err = os.Rename(spool.Name(), archive); err != nil {
		return Info{}, err
	}
	info, err := saveRecord(ctx, archive, id, name, "backup", *m)
	if err != nil {
		os.Remove(archive)
	}
	return info, err
}

type spaceWriter struct {
	File       *os.File
	Directory  string
	untilCheck int64
}

func (w *spaceWriter) Write(bytes []byte) (int, error) {
	if w.untilCheck <= 0 {
		if err := checkSpace(w.Directory, 32<<20); err != nil {
			return 0, err
		}
		w.untilCheck = 32 << 20
	}
	n, err := w.File.Write(bytes)
	w.untilCheck -= int64(n)
	return n, err
}

// SpoolUpload streams multipart content into a configured backup pool rather
// than net/http's system TEMP directory, preserving a free-space reserve.
func SpoolUpload(ctx context.Context, reader io.Reader, poolIDs ...string) (*os.File, func(), error) {
	poolID := ""
	if len(poolIDs) > 0 {
		poolID = poolIDs[0]
	}
	root, err := selectBackupRoot(poolID, 0)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.CreateTemp(root, ".incoming-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { file.Close(); os.Remove(file.Name()) }
	n, err := Copy(ctx, &spaceWriter{File: file, Directory: root}, io.LimitReader(reader, MaxArchiveBytes+1))
	if err == nil && n > MaxArchiveBytes {
		err = fmt.Errorf("upload exceeds 64 GiB limit")
	}
	if err == nil {
		_, err = file.Seek(0, 0)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return file, cleanup, nil
}

// OpenVerified creates an isolated staging tree and revalidates disk checksums
// on every restore, including artifacts that were created on this host.
func OpenVerified(ctx context.Context, id, kind string, validate Validator) (*Manifest, string, func(), error) {
	info, p, err := Resolve(id)
	if err != nil {
		return nil, "", nil, err
	}
	if info.Kind != kind {
		return nil, "", nil, os.ErrNotExist
	}
	digest, err := HashFile(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	if digest != info.SHA256 {
		return nil, "", nil, fmt.Errorf("archive checksum mismatch")
	}
	file, err := os.Open(p)
	if err != nil {
		return nil, "", nil, err
	}
	defer file.Close()
	stage, err := os.MkdirTemp(filepath.Dir(p), ".restore-")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { os.RemoveAll(stage) }
	m, err := Extract(ctx, file, stage)
	if err == nil && validate != nil {
		err = validate(ctx, stage, m)
	}
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return m, stage, cleanup, nil
}

func HashFile(ctx context.Context, p string) (string, error) {
	file, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	_, err = Copy(ctx, h, file)
	return hex.EncodeToString(h.Sum(nil)), err
}

func Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 256<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, e := src.Read(buf)
		if n > 0 {
			written, err := dst.Write(buf[:n])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if e == io.EOF {
			return total, nil
		}
		if e != nil {
			return total, e
		}
	}
}

func EnsureSpace(directory string, required int64) error { return checkSpace(directory, required) }

// NewIdentity intentionally preserves guest accounts but removes all host-side
// addresses, schedules, ownership grants, media references and port mappings.
func NewIdentity(source config.Container, name, poolID string) config.Container {
	c := source
	c.ID = allocateIdentityID()
	c.UUID = config.NewContainerUUID()
	c.Name = name
	c.Standalone = true
	c.LXCName = ""
	c.KVMName = ""
	c.StoragePath = ""
	c.StoragePoolID = poolID
	c.DiskImage = ""
	c.MACAddress = ""
	c.Status = "stopped"
	c.RestoreOnHostBoot = false
	c.IP = ""
	c.LANIPv4Mode = ""
	c.LANInterface = ""
	c.LANIPv4Address = ""
	c.LANIPv4PrefixLen = 0
	c.LANIPv4Gateway = ""
	c.PublicIPv4s = nil
	c.IPv6 = ""
	c.IPv6Addresses = nil
	c.IPv6NATAddress = ""
	c.IPv6PortMappings = nil
	c.IPv6PortMappingLimit = 0
	c.IPv6NATSyncIPv4 = nil
	c.IPv6PrefixLen = 0
	c.IPv6Interface = ""
	c.VNCPort = 0
	c.SSHPort = 0
	c.PortMappings = nil
	c.SSHHostKey = ""
	c.AllowedImageIDs = nil
	c.ImageLimitConfigured = true
	c.FirewallEnabled = false
	c.FirewallRules = nil
	c.FirewallDefaultAction = "DROP"
	c.SnapshotScheduleEnabled = false
	c.SnapshotScheduleLastRun = ""
	c.SnapshotScheduleNextRun = ""
	c.SnapshotScheduleCreatedBy = ""
	c.SnapshotScheduleIntervalHours = 0
	c.SnapshotScheduleTime = ""
	c.TrafficUsedRX = 0
	c.TrafficUsedTX = 0
	c.TrafficResetDate = time.Now().Format("2006-01")
	c.ExpiresAt = ""
	c.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	c.DataDisks = append([]config.DataDisk(nil), source.DataDisks...)
	return c
}
