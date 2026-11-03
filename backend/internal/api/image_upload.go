package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
	"clicd/internal/kvm"
)

const maxLocalImageBytes int64 = 32 << 30

var errLocalImageTooLarge = errors.New("image exceeds upload limit")

type uploadContextReader struct {
	context.Context
	io.Reader
}

func (r uploadContextReader) Read(buffer []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(buffer)
}

func copyImageUpload(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(uploadContextReader{ctx, src}, limit+1))
	if n > limit {
		return n, errLocalImageTooLarge
	}
	if err != nil {
		return n, err
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	if n == 0 {
		return 0, fmt.Errorf("empty image file")
	}
	return n, nil
}

// HandleImageUpload streams into an unregistered temporary cache entry. Only a
// completely received, validated image is atomically published and registered.
func HandleImageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:download") || !requireScope(w, r, "image:toggle") {
		return
	}
	if isAccessRestrictedRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
		return
	}
	if r.Context().Err() != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "上传已取消"})
		return
	}
	if r.ContentLength > maxLocalImageBytes+(1<<20) {
		jsonResponse(w, http.StatusRequestEntityTooLarge, APIResponse{Success: false, Message: "镜像文件不能超过 32 GiB"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLocalImageBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "请使用 multipart/form-data 上传镜像"})
		return
	}
	requestedPool := strings.TrimSpace(r.URL.Query().Get("storage_pool_id"))
	pool, err := config.SelectStoragePoolForContent(config.StorageContentImages, requestedPool, max64(r.ContentLength, 1<<20))
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if requestedPool != "" && pool.ID != requestedPool {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "选定的镜像存储不可用或空间不足，请重新选择"})
		return
	}
	dir := filepath.Join(pool.Path, "images", "kvm")
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "无法创建镜像缓存目录"})
		return
	}
	file, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "无法创建临时上传文件"})
		return
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	fields := map[string]string{}
	hash := sha256.New()
	var received int64
	seenFile := false
	fail := func(message string) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: message})
	}
	for {
		if r.Context().Err() != nil {
			fail("上传已取消")
			return
		}
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			fail("上传未完成或请求超过大小限制")
			return
		}
		if part.FileName() != "" {
			if seenFile || part.FormName() != "file" {
				fail("只允许上传一个 file 文件")
				return
			}
			seenFile = true
			received, err = copyImageUpload(r.Context(), io.MultiWriter(file, hash), part, maxLocalImageBytes)
			if errors.Is(err, errLocalImageTooLarge) {
				jsonResponse(w, http.StatusRequestEntityTooLarge, APIResponse{Success: false, Message: "镜像文件不能超过 32 GiB"})
				return
			}
			if err != nil {
				fail("镜像上传失败、文件为空或超过 32 GiB")
				return
			}
			part.Close()
		} else {
			name := part.FormName()
			switch name {
			case "name", "description", "arch", "sha256", "provisioner", "distro", "release":
			default:
				fail("不支持的上传字段")
				return
			}
			data, readErr := io.ReadAll(io.LimitReader(part, 8193))
			if readErr != nil || len(data) > 8192 {
				fail("上传字段过长")
				return
			}
			part.Close()
			if _, duplicate := fields[name]; duplicate {
				fail("上传字段重复")
				return
			}
			fields[name] = strings.TrimSpace(string(data))
		}
	}
	if !seenFile {
		fail("请选择镜像文件")
		return
	}
	if err := file.Sync(); err != nil {
		fail("镜像写入失败，请检查剩余空间")
		return
	}
	if err := file.Close(); err != nil {
		fail("镜像写入失败")
		return
	}
	name, description := fields["name"], fields["description"]
	if name == "" || utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(description) > 500 {
		fail("镜像名称为 1–100 字，备注最多 500 字")
		return
	}
	if arch := fields["arch"]; arch != "" && arch != runtime.GOARCH {
		fail("镜像架构必须与宿主机一致")
		return
	}
	actualSHA := hex.EncodeToString(hash.Sum(nil))
	if expected := strings.ToLower(fields["sha256"]); expected != "" && expected != actualSHA {
		fail("SHA-256 校验不匹配")
		return
	}
	provisioner := fields["provisioner"]
	format := "qcow2"
	if kvm.ValidateISOFile(temporary) == nil {
		format = "iso"
		if provisioner == "" {
			provisioner = config.KVMProvisionerManualISO
		}
		if provisioner != config.KVMProvisionerManualISO && provisioner != config.KVMProvisionerWindows10 && provisioner != config.KVMProvisionerWindows11 {
			fail("ISO 请选择手动安装或 Windows 自动安装方式")
			return
		}
		if provisioner != config.KVMProvisionerManualISO && runtime.GOARCH != "amd64" {
			fail("Windows 自动安装只支持 amd64；其他 ISO 请使用手动安装")
			return
		}
	} else {
		if err := validateUploadedQCOW2(r.Context(), temporary); err != nil {
			fail(err.Error())
			return
		}
		if provisioner == "" {
			provisioner = "manual-disk"
		}
		if provisioner != "manual-disk" && provisioner != "linux-cloud-init" {
			fail("QCOW2 请选择预装磁盘或 cloud-init 方式")
			return
		}
	}
	distro, release := fields["distro"], fields["release"]
	if distro == "" {
		distro = "generic"
	}
	if release == "" {
		release = "local"
	}
	if provisioner == config.KVMProvisionerWindows10 || provisioner == config.KVMProvisionerWindows11 {
		distro = "windows"
		release = strings.TrimPrefix(provisioner, "windows-")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		fail("无法生成镜像标识")
		return
	}
	id := "custom-kvm-" + hex.EncodeToString(random[:])
	target := filepath.Join(dir, id+"."+format)
	if err := os.Chmod(temporary, 0644); err != nil {
		fail("无法设置镜像权限")
		return
	}
	if r.Context().Err() != nil {
		fail("上传已取消")
		return
	}
	if err := os.Rename(temporary, target); err != nil {
		fail("无法保存镜像")
		return
	}
	registered := false
	defer func() {
		if !registered {
			os.Remove(target)
		}
	}()
	if r.Context().Err() != nil {
		fail("上传已取消")
		return
	}
	image := config.CustomKVMImage{ID: id, Name: name, Description: description, Distro: distro, Release: release, Arch: runtime.GOARCH, Provisioner: provisioner, SHA256: actualSHA, CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if err := config.AddUploadedKVMImage(image); err != nil {
		fail("保存镜像信息失败: " + err.Error())
		return
	}
	registered = true
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Message: "本地镜像已上传、校验并启用", Data: map[string]interface{}{"id": id, "name": name, "format": format, "size_bytes": received, "sha256": actualSHA}})
}

func validateUploadedQCOW2(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var header [104]byte
	n, _ := io.ReadFull(f, header[:])
	defer f.Close()
	if err := validateQCOW2UploadHeader(header[:n]); err != nil {
		return err
	}
	if err := validateQCOW2UploadExtensions(f, header[:n]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", "-f", "qcow2", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("无法验证 QCOW2，请检查文件完整性及 qemu-img 是否可用")
	}
	var info struct {
		Format      string            `json:"format"`
		VirtualSize int64             `json:"virtual-size"`
		Backing     string            `json:"backing-filename"`
		FullBacking string            `json:"full-backing-filename"`
		DataFile    string            `json:"data-file"`
		Encrypted   bool              `json:"encrypted"`
		Snapshots   []json.RawMessage `json:"snapshots"`
		Specific    json.RawMessage   `json:"format-specific"`
	}
	if json.Unmarshal(output, &info) != nil || info.Format != "qcow2" || info.VirtualSize <= 0 || uint64(info.VirtualSize) != binary.BigEndian.Uint64(header[24:32]) || info.Backing != "" || info.FullBacking != "" || info.DataFile != "" || info.Encrypted || len(info.Snapshots) != 0 || strings.Contains(string(info.Specific), `"data-file"`) {
		return fmt.Errorf("只支持独立完整的 QCOW2 磁盘，不允许外部 backing/data 文件")
	}
	if err := exec.CommandContext(ctx, "qemu-img", "check", "-f", "qcow2", path).Run(); err != nil {
		return fmt.Errorf("QCOW2 一致性检查未通过")
	}
	return nil
}

func validateQCOW2UploadHeader(header []byte) error {
	if len(header) < 72 || string(header[:4]) != "QFI\xfb" {
		return fmt.Errorf("文件不是有效 ISO 或 QCOW2 镜像")
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 && version != 3 {
		return fmt.Errorf("不支持的 QCOW2 版本")
	}
	if binary.BigEndian.Uint64(header[8:16]) != 0 || binary.BigEndian.Uint32(header[16:20]) != 0 || binary.BigEndian.Uint32(header[32:36]) != 0 || binary.BigEndian.Uint32(header[60:64]) != 0 || binary.BigEndian.Uint64(header[64:72]) != 0 {
		return fmt.Errorf("QCOW2 必须先展平：不允许 backing、加密或内部快照")
	}
	bits := binary.BigEndian.Uint32(header[20:24])
	virtualSize := binary.BigEndian.Uint64(header[24:32])
	if bits < 9 || bits > 21 || virtualSize == 0 || virtualSize > 16<<40 {
		return fmt.Errorf("QCOW2 文件头或虚拟容量无效（最大 16 TiB）")
	}
	if version == 3 {
		if len(header) < 104 {
			return fmt.Errorf("QCOW2 文件头被截断")
		}
		features := binary.BigEndian.Uint64(header[72:80])
		length := binary.BigEndian.Uint32(header[100:104])
		if features & ^uint64(8|16) != 0 || binary.BigEndian.Uint64(header[88:96])&2 != 0 {
			return fmt.Errorf("QCOW2 不允许外部 data 文件、脏数据或未知的不兼容特性")
		}
		if length < 104 || length%8 != 0 || uint64(length) > uint64(1)<<bits || binary.BigEndian.Uint32(header[96:100]) > 6 || features&16 != 0 && bits < 14 {
			return fmt.Errorf("QCOW2 扩展文件头无效")
		}
	}
	return nil
}

// Header extensions live in the first cluster. Reject host-file references and
// encryption pointers before allowing qemu-img to parse any image metadata.
func validateQCOW2UploadExtensions(file *os.File, header []byte) error {
	length := int64(72)
	if binary.BigEndian.Uint32(header[4:8]) == 3 {
		length = int64(binary.BigEndian.Uint32(header[100:104]))
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	cluster := min(int64(1)<<binary.BigEndian.Uint32(header[20:24]), info.Size())
	data := make([]byte, cluster)
	if _, err := file.ReadAt(data, 0); err != nil {
		return fmt.Errorf("QCOW2 首个 cluster 被截断")
	}
	seen := map[uint32]bool{}
	for offset := length; offset+8 <= int64(len(data)); {
		kind := binary.BigEndian.Uint32(data[offset : offset+4])
		size := int64(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		if kind == 0 {
			if size != 0 {
				return fmt.Errorf("QCOW2 文件头扩展无效")
			}
			return nil
		}
		if kind == 0x44415441 || kind == 0xe2792aca || kind == 0x0537be77 {
			return fmt.Errorf("QCOW2 不允许外部文件或加密文件头扩展")
		}
		if seen[kind] || size > int64(len(data))-offset-8 {
			return fmt.Errorf("QCOW2 文件头扩展越界或重复")
		}
		seen[kind] = true
		offset += 8 + (size+7)/8*8
	}
	return fmt.Errorf("QCOW2 文件头扩展被截断")
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
