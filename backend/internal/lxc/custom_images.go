package lxc

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"clicd/internal/safehttp"
)

type CustomImageDownloadProgress struct {
	Stage           string
	DownloadedBytes int64
	TotalBytes      int64
	Percent         int
}

type CustomImageDownloadProgressFunc func(CustomImageDownloadProgress)

func CustomImagePath(id string) string {
	template := FindTemplate(id)
	if template == nil || !template.Custom {
		return filepath.Join("/var/cache/lxc/download/custom", "__invalid_image_id__", "rootfs.tar")
	}
	return filepath.Join("/var/cache/lxc/download/custom", template.ID, "rootfs.tar")
}

func CustomImageDownloadedInfo(id string) (bool, int64) {
	info, err := os.Stat(CustomImagePath(id))
	if err != nil || info.IsDir() {
		return false, 0
	}
	return true, info.Size()
}

func DeleteCustomImage(id string) error {
	template := FindTemplate(id)
	if template == nil || !template.Custom {
		return fmt.Errorf("custom LXC image not found")
	}
	return os.RemoveAll(filepath.Dir(CustomImagePath(id)))
}

func DownloadCustomImageWithProgress(ctx context.Context, template Template, progress CustomImageDownloadProgressFunc) error {
	if !template.Custom {
		return fmt.Errorf("template is not a custom LXC image")
	}
	target := CustomImagePath(template.ID)
	if ok, _ := CustomImageDownloadedInfo(template.ID); ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	_ = os.Remove(tmp)
	if err := downloadCustomRootfs(ctx, template.URL, tmp, progress); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if template.SHA256 != "" {
		if err := verifyCustomRootfsSHA256(tmp, template.SHA256); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if progress != nil {
		progress(CustomImageDownloadProgress{Stage: "validating", Percent: 100})
	}
	if err := ValidateCustomRootfsArchive(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(target, 0644)
}

func downloadCustomRootfs(ctx context.Context, sourceURL, target string, progress CustomImageDownloadProgressFunc) error {
	response, err := safehttp.Get(ctx, sourceURL, "CLICD/1.0 LXC image downloader", 30*time.Minute)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", response.Status)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	total := response.ContentLength
	buffer := make([]byte, 128*1024)
	var downloaded int64
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, err := file.Write(buffer[:count]); err != nil {
				return err
			}
			downloaded += int64(count)
			if progress != nil {
				percent := 0
				if total > 0 {
					percent = int(downloaded * 100 / total)
					if percent > 100 {
						percent = 100
					}
				}
				progress(CustomImageDownloadProgress{
					Stage:           "downloading",
					DownloadedBytes: downloaded,
					TotalBytes:      total,
					Percent:         percent,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return file.Sync()
}

func verifyCustomRootfsSHA256(filePath, expected string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func ValidateCustomRootfsArchive(archivePath string) error {
	command := exec.Command("tar", "-tf", archivePath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to inspect rootfs archive: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	entries := make([]string, 0, 4096)
	for scanner.Scan() {
		if len(entries) >= 2_000_000 {
			_ = command.Process.Kill()
			return fmt.Errorf("rootfs archive contains too many entries")
		}
		entries = append(entries, scanner.Text())
	}
	scanErr := scanner.Err()
	waitErr := command.Wait()
	if scanErr != nil {
		return fmt.Errorf("failed to read rootfs archive: %v", scanErr)
	}
	if waitErr != nil {
		return fmt.Errorf("invalid rootfs archive: %v, output: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return validateCustomRootfsEntries(entries)
}

func validateCustomRootfsEntries(entries []string) error {
	hasInit := false
	for _, entry := range entries {
		entry = strings.TrimSpace(strings.ReplaceAll(entry, "\\", "/"))
		entry = strings.TrimPrefix(entry, "./")
		if entry == "" || entry == "." {
			continue
		}
		if strings.HasPrefix(entry, "/") {
			return fmt.Errorf("rootfs archive contains an absolute path: %s", entry)
		}
		clean := path.Clean(entry)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("rootfs archive contains path traversal: %s", entry)
		}
		switch strings.TrimSuffix(clean, "/") {
		case "sbin/init", "usr/lib/systemd/systemd", "lib/systemd/systemd", "bin/busybox", "bin/sh":
			hasInit = true
		}
	}
	if len(entries) == 0 {
		return fmt.Errorf("rootfs archive is empty")
	}
	if !hasInit {
		return fmt.Errorf("rootfs archive does not contain a supported init")
	}
	return nil
}

func ExtractCustomRootfs(templateID, destination string) error {
	template := FindTemplate(templateID)
	if template == nil || !template.Custom {
		return fmt.Errorf("custom LXC image not found: %s", templateID)
	}
	archive := CustomImagePath(template.ID)
	if ok, _ := CustomImageDownloadedInfo(template.ID); !ok {
		return fmt.Errorf("custom LXC image is not downloaded: %s", templateID)
	}
	if err := ValidateCustomRootfsArchive(archive); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	output, err := exec.Command("tar", "-xpf", archive, "-C", destination).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract custom LXC rootfs: %v, output: %s", err, strings.TrimSpace(string(output)))
	}
	if err := secureExtractedRootfs(destination); err != nil {
		return err
	}
	if !rootfsHasInit(destination) {
		return fmt.Errorf("extracted custom LXC rootfs is invalid: init not found")
	}
	return nil
}

func secureExtractedRootfs(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := os.Readlink(filePath)
		if err != nil {
			return err
		}
		var resolved string
		if filepath.IsAbs(target) {
			resolved = filepath.Join(root, strings.TrimLeft(filepath.ToSlash(target), "/"))
			relative, err := filepath.Rel(filepath.Dir(filePath), resolved)
			if err != nil {
				return err
			}
			if err := os.Remove(filePath); err != nil {
				return err
			}
			if err := os.Symlink(relative, filePath); err != nil {
				return err
			}
		} else {
			resolved = filepath.Join(filepath.Dir(filePath), target)
		}
		relativeToRoot, err := filepath.Rel(root, filepath.Clean(resolved))
		if err != nil {
			return err
		}
		if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("rootfs symlink escapes the archive root: %s -> %s", filePath, target)
		}
		return nil
	})
}
