package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nwaples/rardecode/v2"
)

const (
	maxZipEntrySize  = 4 << 30 // 4 GB per ROM
	maxTotalExtract  = 8 << 30 // 8 GB total per zip
	maxZipEntries    = 100000
)

var dedupCounter atomic.Int64

func init() {
	dedupCounter.Store(time.Now().UnixNano())
}

var romFolders = map[string]string{
	".gba": "GBA",
	".nds": "NDS",
	".3ds": "3DS",
	".cia": "3DS",
}

func isROM(filename string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	folder, ok := romFolders[ext]
	return folder, ok
}

func isZipFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".zip"
}

func isRarFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".rar"
}

func isArchive(path string) bool {
	return isZipFile(path) || isRarFile(path)
}

func sanitizePath(path string) string {
	if path == "" {
		return ""
	}
	if len(path) >= 2 && path[1] == ':' {
		path = path[2:]
	}
	cleaned := filepath.Clean(path)
	cleaned = strings.TrimLeft(cleaned, "\\/")
	for {
		if cleaned == ".." {
			return ""
		}
		if strings.HasPrefix(cleaned, "..\\") || strings.HasPrefix(cleaned, "../") {
			cleaned = cleaned[3:]
			continue
		}
		break
	}
	return cleaned
}

func deduplicateName(filename string) string {
	ext := filepath.Ext(filename)
	base := filename[:len(filename)-len(ext)]
	return fmt.Sprintf("%s_%d%s", base, dedupCounter.Add(1), ext)
}

func isWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(filepath.Dir(dst), deduplicateName(filepath.Base(dst)))
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Cross-drive fallback: copy then delete
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}

	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Close()
}

func extractZip(zipPath, destDir string) ([]string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", zipPath, err)
	}
	defer reader.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return nil, fmt.Errorf("abs dest dir: %w", err)
	}

	if len(reader.File) > maxZipEntries {
		return nil, fmt.Errorf("zip contains too many entries: %d (max %d)", len(reader.File), maxZipEntries)
	}

	var extracted []string
	var totalSize int64

	for _, f := range reader.File {
		// Skip symlinks, devices, and other non-regular files
		if f.Mode()&os.ModeSymlink != 0 || !f.Mode().IsRegular() && !f.Mode().IsDir() {
			continue
		}

		safeName := sanitizePath(f.Name)
		if safeName == "" || safeName == "." {
			continue
		}

		destPath := filepath.Join(destDir, safeName)

		absDestPath, err := filepath.Abs(destPath)
		if err != nil {
			return extracted, fmt.Errorf("abs dest path %q: %w", destPath, err)
		}
		if !isWithinDir(absDestPath, absDestDir) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return extracted, fmt.Errorf("mkdir %q: %w", destPath, err)
			}
			continue
		}

		if f.UncompressedSize64 > maxZipEntrySize {
			return extracted, fmt.Errorf("zip entry %q too large: %d bytes (max %d)", f.Name, f.UncompressedSize64, maxZipEntrySize)
		}
		if totalSize+int64(f.UncompressedSize64) > maxTotalExtract {
			return extracted, fmt.Errorf("total extraction size exceeds limit of %d bytes", maxTotalExtract)
		}
		totalSize += int64(f.UncompressedSize64)

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return extracted, fmt.Errorf("mkdir parent %q: %w", filepath.Dir(destPath), err)
		}

		rc, err := f.Open()
		if err != nil {
			return extracted, fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return extracted, fmt.Errorf("create %q: %w", destPath, err)
		}

		_, copyErr := io.CopyN(outFile, rc, int64(f.UncompressedSize64))
		rc.Close()
		closeErr := outFile.Close()

		if copyErr != nil {
			os.Remove(destPath)
			return extracted, fmt.Errorf("write %q: %w", destPath, copyErr)
		}
		if closeErr != nil {
			os.Remove(destPath)
			return extracted, fmt.Errorf("close %q: %w", destPath, closeErr)
		}

		extracted = append(extracted, destPath)
	}

	return extracted, nil
}

func extractRar(rarPath, destDir string) ([]string, error) {
	reader, err := rardecode.OpenReader(rarPath)
	if err != nil {
		return nil, fmt.Errorf("open rar %q: %w", rarPath, err)
	}
	defer reader.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return nil, fmt.Errorf("abs dest dir: %w", err)
	}

	var extracted []string
	var totalSize int64

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return extracted, fmt.Errorf("read rar entry: %w", err)
		}

		// Skip symlinks, devices, and other non-regular files
		if header.Mode()&os.ModeSymlink != 0 || !header.Mode().IsRegular() && !header.IsDir {
			continue
		}

		safeName := sanitizePath(header.Name)
		if safeName == "" || safeName == "." {
			continue
		}

		destPath := filepath.Join(destDir, safeName)

		absDestPath, err := filepath.Abs(destPath)
		if err != nil {
			return extracted, fmt.Errorf("abs dest path %q: %w", destPath, err)
		}
		if !isWithinDir(absDestPath, absDestDir) {
			continue
		}

		if header.IsDir {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return extracted, fmt.Errorf("mkdir %q: %w", destPath, err)
			}
			continue
		}

		// RAR doesn't always report uncompressed size upfront
		uncompressedSize := int64(header.UnPackedSize)
		if uncompressedSize > 0 && uncompressedSize > maxZipEntrySize {
			return extracted, fmt.Errorf("rar entry %q too large: %d bytes (max %d)", header.Name, uncompressedSize, maxZipEntrySize)
		}
		if totalSize+uncompressedSize > maxTotalExtract {
			return extracted, fmt.Errorf("total extraction size exceeds limit of %d bytes", maxTotalExtract)
		}
		totalSize += uncompressedSize

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return extracted, fmt.Errorf("mkdir parent %q: %w", filepath.Dir(destPath), err)
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			return extracted, fmt.Errorf("create %q: %w", destPath, err)
		}

		_, copyErr := io.CopyN(outFile, reader, uncompressedSize)
		closeErr := outFile.Close()

		if copyErr != nil {
			os.Remove(destPath)
			return extracted, fmt.Errorf("write %q: %w", destPath, copyErr)
		}
		if closeErr != nil {
			os.Remove(destPath)
			return extracted, fmt.Errorf("close %q: %w", destPath, closeErr)
		}

		extracted = append(extracted, destPath)
	}

	return extracted, nil
}

func processArchive(archivePath, baseDir string) error {
	tmpDir, err := os.MkdirTemp("", "rom-sorter")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	os.Chmod(tmpDir, 0700)
	defer os.RemoveAll(tmpDir)

	var files []string
	if isZipFile(archivePath) {
		files, err = extractZip(archivePath, tmpDir)
	} else if isRarFile(archivePath) {
		files, err = extractRar(archivePath, tmpDir)
	} else {
		return fmt.Errorf("unsupported archive format: %s", archivePath)
	}
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	for _, file := range files {
		filename := filepath.Base(file)
		folder, isROM := isROM(filename)
		if !isROM {
			continue
		}

		destDir := filepath.Join(baseDir, folder)
		destPath := filepath.Join(destDir, filename)

		if err := moveFile(file, destPath); err != nil {
			return fmt.Errorf("move %q to %q: %w", filename, destDir, err)
		}
	}

	if err := os.Remove(archivePath); err != nil {
		return fmt.Errorf("remove archive %q: %w", archivePath, err)
	}

	return nil
}

func processZip(zipPath, baseDir string) error {
	tmpDir, err := os.MkdirTemp("", "rom-sorter")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	os.Chmod(tmpDir, 0700)
	defer os.RemoveAll(tmpDir)

	files, err := extractZip(zipPath, tmpDir)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	for _, file := range files {
		filename := filepath.Base(file)
		folder, isROM := isROM(filename)
		if !isROM {
			continue
		}

		destDir := filepath.Join(baseDir, folder)
		destPath := filepath.Join(destDir, filename)

		if err := moveFile(file, destPath); err != nil {
			return fmt.Errorf("move %q to %q: %w", filename, destDir, err)
		}
	}

	if err := os.Remove(zipPath); err != nil {
		return fmt.Errorf("remove zip %q: %w", zipPath, err)
	}

	return nil
}

func run(baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("read dir %q: %w", baseDir, err)
	}

	var errs []error
	processed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isArchive(entry.Name()) {
			continue
		}
		archivePath := filepath.Join(baseDir, entry.Name())
		fmt.Printf("Processing: %s\n", entry.Name())
		if err := processArchive(archivePath, baseDir); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Name(), err))
			fmt.Printf("  ERROR: %v\n", err)
			continue
		}
		fmt.Printf("  Done.\n")
		processed++
	}

	if processed == 0 {
		fmt.Printf("No archives (.zip/.rar) found in %s\n", baseDir)
	}

	if len(errs) > 0 {
		return fmt.Errorf("finished with %d error(s): %w", len(errs), errors.Join(errs...))
	}

	return nil
}

func mainImpl(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	fmt.Printf("ROM Sorter - Scanning %s for archives (.zip/.rar)\n", absDir)
	if err := run(absDir); err != nil {
		return err
	}
	return nil
}

func main() {
	interactive := len(os.Args) == 1
	baseDir := "."
	if len(os.Args) > 1 {
		baseDir = os.Args[1]
	}

	if err := mainImpl(baseDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if interactive {
			fmt.Fprint(os.Stderr, "\nPress Enter to exit...")
			fmt.Scanln()
		}
		os.Exit(1)
	}

	fmt.Println("Done.")
	if interactive {
		fmt.Print("Press Enter to exit...")
		fmt.Scanln()
	}
}
