package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "commit-guard.exe"
	}
	return "commit-guard"
}

func installedCommand(home, ref string) string {
	if !ValidRef(ref) {
		return "commit-guard"
	}
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "& "
	}
	return prefix + `"` + filepath.ToSlash(filepath.Join(home, "versions", ref, BinaryName())) + `"`
}

func fetch(url string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "commit-guard-installer")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("download exceeds %d bytes", limit)
	}
	return data, err
}

func ResolveVersion(ref string) (string, error) {
	if !ValidRef(ref) {
		return "", fmt.Errorf("invalid release reference %q", ref)
	}
	if releaseRef.MatchString(ref) {
		return strings.TrimPrefix(ref, "v"), nil
	}
	data, err := fetch("https://raw.githubusercontent.com/"+Repository+"/"+ref+"/VERSION", 1024)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if !releaseRef.MatchString("v" + version) {
		return "", fmt.Errorf("invalid VERSION at %s", ref)
	}
	return version, nil
}

func Download(version string) ([]byte, string, error) {
	if !releaseRef.MatchString("v" + version) {
		return nil, "", fmt.Errorf("invalid release version")
	}
	arch := runtime.GOARCH
	if arch == "386" && runtime.GOOS == "windows" {
		arch = "amd64"
	}
	name := fmt.Sprintf("commit-guard_%s_%s_%s", version, runtime.GOOS, arch)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	base := "https://github.com/" + Repository + "/releases/download/v" + version + "/"
	sums, err := fetch(base+"SHA256SUMS", 64*1024)
	if err != nil {
		return nil, "", err
	}
	expected := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			expected = fields[0]
		}
	}
	if len(expected) != 64 {
		return nil, "", fmt.Errorf("release checksum missing for %s", name)
	}
	data, err := fetch(base+name, 32*1024*1024)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	if actual != expected {
		return nil, "", fmt.Errorf("release checksum mismatch for %s", name)
	}
	return data, actual, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".commit-guard-*")
	if err != nil {
		return err
	}
	tempPath := tmp.Name()
	defer os.Remove(tempPath)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
