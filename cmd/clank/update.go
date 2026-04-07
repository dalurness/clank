package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const releaseURL = "https://api.github.com/repos/dalurness/clank/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func cmdUpdate() int {
	// Determine expected asset name for this platform.
	assetName, err := platformAssetName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Fetch latest release metadata.
	fmt.Println("checking for updates...")
	release, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not check for updates: %v\n", err)
		return 1
	}

	// Compare versions.
	if Version == release.TagName {
		fmt.Printf("clank is already up to date (%s)\n", Version)
		return 0
	}
	fmt.Printf("updating clank: %s -> %s\n", Version, release.TagName)

	// Find the matching asset.
	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "error: no release asset found for %s\n", assetName)
		return 1
	}

	// Resolve current executable path.
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not determine executable path: %v\n", err)
		return 1
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not resolve executable path: %v\n", err)
		return 1
	}

	// Download to a temp file in the same directory (so rename is same-device).
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, "clank-update-*")
	if err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "error: permission denied writing to %s\ntry again with sudo (or your platform equivalent)\n", dir)
			return 1
		}
		fmt.Fprintf(os.Stderr, "error: could not create temp file: %v\n", err)
		return 1
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // clean up on failure

	if err := downloadFile(tmp, downloadURL); err != nil {
		tmp.Close()
		fmt.Fprintf(os.Stderr, "error: download failed: %v\n", err)
		return 1
	}
	tmp.Close()

	// Make executable on Unix.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error: could not set executable permission: %v\n", err)
			return 1
		}
	}

	// Replace binary.
	if err := replaceBinary(exePath, tmpPath); err != nil {
		if isPermissionError(err) {
			fmt.Fprintf(os.Stderr, "error: permission denied writing to %s\ntry again with sudo (or your platform equivalent)\n", exePath)
			return 1
		}
		fmt.Fprintf(os.Stderr, "error: could not replace binary: %v\n", err)
		return 1
	}

	fmt.Printf("updated clank: %s -> %s\n", Version, release.TagName)
	return 0
}

func platformAssetName() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}

	supported := map[string]bool{
		"linux/amd64":   true,
		"linux/arm64":   true,
		"darwin/amd64":  true,
		"darwin/arm64":  true,
		"windows/amd64": true,
	}
	key := goos + "/" + goarch
	if !supported[key] {
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
	return fmt.Sprintf("clank-%s-%s%s", goos, goarch, ext), nil
}

func fetchLatestRelease() (*ghRelease, error) {
	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("could not parse release data: %w", err)
	}
	return &release, nil
}

func downloadFile(dst *os.File, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	_, err = io.Copy(dst, resp.Body)
	return err
}

func replaceBinary(oldPath, newPath string) error {
	if runtime.GOOS == "windows" {
		// Windows can't overwrite a running exe, but can rename it.
		bakPath := oldPath + ".old"
		// Remove any leftover .old from a previous update.
		os.Remove(bakPath)
		if err := os.Rename(oldPath, bakPath); err != nil {
			return err
		}
		if err := os.Rename(newPath, oldPath); err != nil {
			// Try to restore the old binary.
			os.Rename(bakPath, oldPath)
			return err
		}
		// Best-effort cleanup of .old file.
		os.Remove(bakPath)
		return nil
	}
	// Unix: atomic rename.
	return os.Rename(newPath, oldPath)
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "access is denied")
}
