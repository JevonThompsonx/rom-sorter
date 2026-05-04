package main

import (
	"archive/zip"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func TestIsROM(t *testing.T) {
	tests := []struct {
		filename string
		isROM    bool
		folder   string
	}{
		{"game.gba", true, "GBA"},
		{"game.GBA", true, "GBA"},
		{"game.nds", true, "NDS"},
		{"game.NDS", true, "NDS"},
		{"game.3ds", true, "3DS"},
		{"game.cia", true, "3DS"},
		{"game.CIA", true, "3DS"},
		{"readme.txt", false, ""},
		{"game.exe", false, ""},
		{"game.sav", false, ""},
		{"noextension", false, ""},
		{"path/to/game.gba", true, "GBA"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			folder, ok := isROM(tt.filename)
			if ok != tt.isROM {
				t.Errorf("isROM(%q) ok = %v, want %v", tt.filename, ok, tt.isROM)
			}
			if folder != tt.folder {
				t.Errorf("isROM(%q) folder = %q, want %q", tt.filename, folder, tt.folder)
			}
		})
	}
}

func TestIsROM_NonROMExtensions(t *testing.T) {
	nonROMs := []string{".sav", ".txt", ".md", ".png", ".jpg", ".exe", ".dll", ".ini", ".cfg", ".xml"}
	for _, ext := range nonROMs {
		if _, ok := isROM("file" + ext); ok {
			t.Errorf("isROM(file%s) should not be a ROM", ext)
		}
	}
}

func TestZipSlipSanitize(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"../../../etc/passwd", "passwd"},
		{"..\\..\\Windows\\system32\\evil.exe", "evil.exe"},
		{"/absolute/path/game.gba", "game.gba"},
		{"C:\\absolute\\path\\game.nds", "game.nds"},
		{"game.gba", "game.gba"},
		{"subfolder/game.gba", "subfolder\\game.gba"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePath(tt.name)
			if filepath.IsAbs(result) {
				t.Errorf("sanitizePath(%q) returned absolute path %q", tt.name, result)
			}
			if filepath.Base(result) != filepath.Base(tt.expected) {
				t.Errorf("sanitizePath(%q) base = %q, want %q", tt.name, filepath.Base(result), filepath.Base(tt.expected))
			}
		})
	}
}

func TestDeduplicateName(t *testing.T) {
	filename := "game.gba"
	result := deduplicateName(filename)
	if result == filename {
		t.Errorf("deduplicateName(%q) should add timestamp suffix", filename)
	}
}

func TestDeduplicateName_Unique(t *testing.T) {
	// Rapid consecutive calls should produce unique names (nanosecond resolution)
	r1 := deduplicateName("game.gba")
	r2 := deduplicateName("game.gba")
	if r1 == r2 {
		t.Errorf("deduplicateName should produce unique names on consecutive calls, got %q and %q", r1, r2)
	}
}

func TestIsZipFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"roms.zip", true},
		{"roms.ZIP", true},
		{"roms.Zip", true},
		{"roms.rar", false},
		{"roms.7z", false},
		{"roms.txt", false},
		{"roms.zip.crdownload", false},
		{"roms.zip.tmp", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isZipFile(tt.path); got != tt.expected {
				t.Errorf("isZipFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestIsZipFile_Part(t *testing.T) {
	if isZipFile("roms.zip.part") {
		t.Error("isZipFile should return false for .zip.part")
	}
}

func TestSanitizePath_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"..", ""},
		{"../", ""},
		{"../..", ""},
		{"../../..", ""},
		{".", "."},
		{"", ""},
		{"D:\\games\\rom.gba", "games\\rom.gba"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizePath(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsWithinDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-within")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)

	tests := []struct {
		path     string
		dir      string
		expected bool
	}{
		{filepath.Join(subDir, "file.gba"), tmpDir, true},
		{filepath.Join(subDir, "nested", "file.gba"), tmpDir, true},
		{filepath.Join(tmpDir, "..", "escaped", "file.gba"), tmpDir, false},
		{filepath.Join(tmpDir+"2", "file.gba"), tmpDir, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isWithinDir(tt.path, tt.dir); got != tt.expected {
				t.Errorf("isWithinDir(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.expected)
			}
		})
	}
}

func TestExtractZip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "test.zip")
	createTestZip(t, zipPath, map[string]string{
		"game.gba":     "fake gba content",
		"game.nds":     "fake nds content",
		"readme.txt":   "this is not a rom",
		"sub/game.3ds": "nested 3ds rom",
	})

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractZip(zipPath, destDir)
	if err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	if len(files) != 4 {
		t.Errorf("extractZip returned %d files, want 4", len(files))
	}

	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("extracted file %q does not exist", f)
		}
	}
}

func TestExtractZip_SlipPrevention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-slip")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "evil.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}
	w := zip.NewWriter(f)
	header := &zip.FileHeader{
		Name:   "../../../evil.exe",
		Method: zip.Store,
	}
	header.SetMode(0644)
	writer, err := w.CreateHeader(header)
	if err != nil {
		t.Fatalf("failed to create header: %v", err)
	}
	writer.Write([]byte("malicious"))
	w.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractZip(zipPath, destDir)
	if err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	for _, file := range files {
		if !isWithinDir(file, destDir) {
			t.Errorf("zip-slip detected: %q escaped %q", file, destDir)
		}
	}
}

func TestExtractZip_WithDirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-zipdir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "withdirs.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}
	w := zip.NewWriter(f)

	dirHeader := &zip.FileHeader{Name: "roms/", Method: zip.Store}
	dirHeader.SetMode(0755 | os.ModeDir)
	if _, err := w.CreateHeader(dirHeader); err != nil {
		t.Fatalf("failed to create dir header: %v", err)
	}

	fileWriter, _ := w.Create("roms/game.gba")
	fileWriter.Write([]byte("rom content"))

	w.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractZip(zipPath, destDir)
	if err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}

	checkFileExists(t, filepath.Join(destDir, "roms", "game.gba"))
}

func TestExtractZip_InvalidZip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-invalid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	badZip := filepath.Join(tmpDir, "bad.zip")
	if err := os.WriteFile(badZip, []byte("not a zip file"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	destDir := filepath.Join(tmpDir, "extracted")
	_, err = extractZip(badZip, destDir)
	if err == nil {
		t.Error("expected error for invalid zip")
	}
}

func TestExtractZip_NonExistentZip(t *testing.T) {
	_, err := extractZip("nonexistent.zip", "dest")
	if err == nil {
		t.Error("expected error for non-existent zip")
	}
}

func TestExtractZip_Symlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-symlink")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "symlink.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}
	w := zip.NewWriter(f)

	header := &zip.FileHeader{
		Name:   "link_to_gba",
		Method: zip.Store,
	}
	header.SetMode(0644 | os.ModeSymlink)
	if _, err := w.CreateHeader(header); err != nil {
		t.Fatalf("failed to create symlink header: %v", err)
	}

	reg, _ := w.Create("game.gba")
	reg.Write([]byte("rom"))

	w.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractZip(zipPath, destDir)
	if err != nil {
		t.Fatalf("extractZip failed: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 extracted file, got %d", len(files))
	}
}

func TestCrossDriveMove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-move")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "source.gba")
	content := []byte("fake gba rom")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	destFile := filepath.Join(destDir, "source.gba")

	if err := moveFile(srcFile, destFile); err != nil {
		t.Fatalf("moveFile failed: %v", err)
	}

	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("source file should not exist after move")
	}

	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("dest content = %q, want %q", string(data), string(content))
	}
}

func TestCrossDriveMove_DuplicateName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-dup")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "game.gba")
	if err := os.WriteFile(srcFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	existingFile := filepath.Join(destDir, "game.gba")
	if err := os.WriteFile(existingFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("failed to write existing: %v", err)
	}

	destFile := filepath.Join(destDir, "game.gba")
	if err := moveFile(srcFile, destFile); err != nil {
		t.Fatalf("moveFile failed: %v", err)
	}

	entries, _ := os.ReadDir(destDir)
	if len(entries) < 2 {
		t.Errorf("expected at least 2 files in dest dir, got %d", len(entries))
	}
}

func TestMoveFile_LargeFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-large")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "big.nds")
	content := make([]byte, 1024*64)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	destDir := filepath.Join(tmpDir, "target")
	destFile := filepath.Join(destDir, "big.nds")

	if err := moveFile(srcFile, destFile); err != nil {
		t.Fatalf("moveFile failed: %v", err)
	}

	data, _ := os.ReadFile(destFile)
	if len(data) != len(content) {
		t.Errorf("size mismatch: got %d, want %d", len(data), len(content))
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-copy")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "source.gba")
	content := []byte("copy test data")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	dst := filepath.Join(tmpDir, "copied.gba")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	if _, err := os.Stat(src); os.IsNotExist(err) {
		t.Error("source should still exist after copy")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("dest content = %q, want %q", string(data), string(content))
	}
}

func TestCopyFile_Errors(t *testing.T) {
	t.Run("non-existent source", func(t *testing.T) {
		err := copyFile("nonexistent.gba", "dest.gba")
		if err == nil {
			t.Error("expected error for non-existent source")
		}
	})

	t.Run("invalid destination path", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "rom-sorter-copyerr")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		src := filepath.Join(tmpDir, "source.gba")
		if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
			t.Fatalf("failed to write source: %v", err)
		}

		err = copyFile(src, filepath.Join(tmpDir, "nonexistent", "dest.gba"))
		if err == nil {
			t.Error("expected error for invalid destination")
		}
	})

	t.Run("destination already exists", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "rom-sorter-copydst")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		src := filepath.Join(tmpDir, "source.gba")
		dst := filepath.Join(tmpDir, "existing.gba")
		os.WriteFile(src, []byte("src"), 0644)
		os.WriteFile(dst, []byte("dst"), 0644)

		if err := copyFile(src, dst); err == nil {
			t.Error("expected error when destination already exists")
		}
	})
}

func TestProcessZip_Integration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-integration")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	zipPath := filepath.Join(tmpDir, "collection.zip")
	createTestZip(t, zipPath, map[string]string{
		"mario.gba":       "mario rom",
		"zelda.nds":       "zelda rom",
		"pokemon.3ds":     "pokemon rom",
		"fireemblem.cia":  "fire emblem rom",
		"manual.pdf":      "not a rom",
		"save_data.sav":   "not a rom",
	})

	if err := processZip(zipPath, tmpDir); err != nil {
		t.Fatalf("processZip failed: %v", err)
	}

	checkFileNotExists(t, zipPath)

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "mario.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "zelda.nds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "pokemon.3ds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "fireemblem.cia"))

	checkFileNotExists(t, filepath.Join(tmpDir, "GBA", "manual.pdf"))
	checkFileNotExists(t, filepath.Join(tmpDir, "GBA", "save_data.sav"))
}

func TestProcessZip_DeduplicateExistingROM(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-dedup")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gbaDir := filepath.Join(tmpDir, "GBA")
	os.MkdirAll(gbaDir, 0755)

	existingROM := filepath.Join(gbaDir, "game.gba")
	if err := os.WriteFile(existingROM, []byte("already here"), 0644); err != nil {
		t.Fatalf("failed to write existing: %v", err)
	}

	zipPath := filepath.Join(tmpDir, "collection.zip")
	createTestZip(t, zipPath, map[string]string{
		"game.gba": "new version",
	})

	if err := processZip(zipPath, tmpDir); err != nil {
		t.Fatalf("processZip failed: %v", err)
	}

	checkFileExists(t, existingROM)

	entries, err := os.ReadDir(gbaDir)
	if err != nil {
		t.Fatalf("read gba dir: %v", err)
	}
	gbaFiles := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".gba" {
			gbaFiles++
		}
	}
	if gbaFiles < 2 {
		t.Errorf("expected at least 2 .gba files (original + deduplicated), got %d", gbaFiles)
	}
}

func TestProcessZip_OnlyNonROMs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-nonroms")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "GBA"), 0755)

	zipPath := filepath.Join(tmpDir, "docs.zip")
	createTestZip(t, zipPath, map[string]string{
		"readme.txt":  "instructions",
		"manual.pdf":  "pdf content",
		"config.ini":  "settings",
	})

	if err := processZip(zipPath, tmpDir); err != nil {
		t.Fatalf("processZip failed: %v", err)
	}

	checkFileNotExists(t, zipPath)

	entries, _ := os.ReadDir(filepath.Join(tmpDir, "GBA"))
	if len(entries) > 0 {
		t.Errorf("GBA dir should be empty, got %d entries", len(entries))
	}
}

func TestProcessZip_CorruptZip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-corrupt")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	badZip := filepath.Join(tmpDir, "corrupt.zip")
	if err := os.WriteFile(badZip, []byte("not a valid zip"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err = processZip(badZip, tmpDir)
	if err == nil {
		t.Error("expected error for corrupt zip")
	}
}

func TestProcessZip_Subdirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-subdir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	zipPath := filepath.Join(tmpDir, "nested.zip")
	createTestZip(t, zipPath, map[string]string{
		"batch1/game1.gba": "gba1",
		"batch2/game2.nds": "nds1",
	})

	if err := processZip(zipPath, tmpDir); err != nil {
		t.Fatalf("processZip failed: %v", err)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "game1.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "game2.nds"))
}

func TestRun_EmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-empty")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := run(tmpDir); err != nil {
		t.Errorf("run on empty dir should not error, got: %v", err)
	}
}

func TestRun_MixedContent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-mixed")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	createTestZip(t, filepath.Join(tmpDir, "gba_games.zip"), map[string]string{
		"fireemblem.gba":  "fe rom",
		"advancewars.gba": "aw rom",
	})
	createTestZip(t, filepath.Join(tmpDir, "nds_games.zip"), map[string]string{
		"mariokart.nds": "mk rom",
	})

	os.WriteFile(filepath.Join(tmpDir, "ignored.7z"), []byte("not supported"), 0644)

	if err := run(tmpDir); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "fireemblem.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "GBA", "advancewars.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "mariokart.nds"))

	checkFileNotExists(t, filepath.Join(tmpDir, "gba_games.zip"))
	checkFileNotExists(t, filepath.Join(tmpDir, "nds_games.zip"))
	checkFileExists(t, filepath.Join(tmpDir, "ignored.7z"))
}

func TestRun_WithInvalidZip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-runbad")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "bad.zip"), []byte("not a zip"), 0644)

	if err := run(tmpDir); err == nil {
		t.Error("run should return an error for invalid zips")
	}
}

func TestRun_SuccessNoErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-runsuccess")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "GBA"), 0755)

	createTestZip(t, filepath.Join(tmpDir, "games.zip"), map[string]string{
		"game.gba": "content",
	})

	if err := run(tmpDir); err != nil {
		t.Errorf("run should not error on successful processing, got: %v", err)
	}
}

func TestMainImpl(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-main")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "GBA"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "NDS"), 0755)

	createTestZip(t, filepath.Join(tmpDir, "games.zip"), map[string]string{
		"pokemon.gba": "poke data",
		"zelda.nds":   "zelda data",
	})

	if err := mainImpl(tmpDir); err != nil {
		t.Fatalf("mainImpl failed: %v", err)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "pokemon.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "zelda.nds"))
	checkFileNotExists(t, filepath.Join(tmpDir, "games.zip"))
}

func TestMainImpl_InvalidPath(t *testing.T) {
	err := mainImpl("NUL:")
	if err == nil {
		t.Error("mainImpl should fail on invalid path")
	}
}

// --- RAR Tests (Phase: RED — will fail until RAR support is implemented) ---

func TestIsRarFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"roms.rar", true},
		{"roms.RAR", true},
		{"roms.Rar", true},
		{"roms.zip", false},
		{"roms.7z", false},
		{"roms.txt", false},
		{"roms.rar.part", false},
		{"roms.rar.crdownload", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isRarFile(tt.path); got != tt.expected {
				t.Errorf("isRarFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestIsArchive(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"roms.zip", true},
		{"roms.rar", true},
		{"roms.ZIP", true},
		{"roms.RAR", true},
		{"roms.7z", false},
		{"roms.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isArchive(tt.path); got != tt.expected {
				t.Errorf("isArchive(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestExtractRar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-rar")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rarPath := filepath.Join(tmpDir, "test.rar")
	createTestRar(t, rarPath, map[string]string{
		"game.gba":     "fake gba content",
		"game.nds":     "fake nds content",
		"readme.txt":   "this is not a rom",
		"sub/game.3ds": "nested 3ds rom",
	})

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractRar(rarPath, destDir)
	if err != nil {
		t.Fatalf("extractRar failed: %v", err)
	}

	if len(files) != 4 {
		t.Errorf("extractRar returned %d files, want 4", len(files))
	}

	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("extracted file %q does not exist", f)
		}
	}
}

func TestExtractRar_SlipPrevention(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-rarslip")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rarPath := filepath.Join(tmpDir, "evil.rar")
	createTestRarSlip(t, rarPath, "../../../evil.exe", []byte("malicious"))

	destDir := filepath.Join(tmpDir, "extracted")
	files, err := extractRar(rarPath, destDir)
	if err != nil {
		t.Fatalf("extractRar failed: %v", err)
	}

	for _, file := range files {
		if !isWithinDir(file, destDir) {
			t.Errorf("rar-slip detected: %q escaped %q", file, destDir)
		}
	}
}

func TestExtractRar_InvalidRar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-rarinvalid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	badRar := filepath.Join(tmpDir, "bad.rar")
	if err := os.WriteFile(badRar, []byte("not a rar file"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	destDir := filepath.Join(tmpDir, "extracted")
	_, err = extractRar(badRar, destDir)
	if err == nil {
		t.Error("expected error for invalid rar")
	}
}

func TestExtractRar_NonExistentRar(t *testing.T) {
	_, err := extractRar("nonexistent.rar", "dest")
	if err == nil {
		t.Error("expected error for non-existent rar")
	}
}

func TestProcessArchive_IntegrationRar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-archive-rar")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	rarPath := filepath.Join(tmpDir, "collection.rar")
	createTestRar(t, rarPath, map[string]string{
		"mario.gba":      "mario rom",
		"zelda.nds":      "zelda rom",
		"pokemon.3ds":    "pokemon rom",
		"fireemblem.cia": "fire emblem rom",
		"manual.pdf":     "not a rom",
	})

	if err := processArchive(rarPath, tmpDir); err != nil {
		t.Fatalf("processArchive (rar) failed: %v", err)
	}

	checkFileNotExists(t, rarPath)
	checkFileExists(t, filepath.Join(tmpDir, "GBA", "mario.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "zelda.nds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "pokemon.3ds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "fireemblem.cia"))
	checkFileNotExists(t, filepath.Join(tmpDir, "GBA", "manual.pdf"))
}

func TestRun_MixedArchives(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-mixedarch")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	createTestZip(t, filepath.Join(tmpDir, "gba_games.zip"), map[string]string{
		"fireemblem.gba": "fe rom",
	})
	createTestRar(t, filepath.Join(tmpDir, "nds_games.rar"), map[string]string{
		"mariokart.nds": "mk rom",
	})
	os.WriteFile(filepath.Join(tmpDir, "ignored.7z"), []byte("not supported"), 0644)

	if err := run(tmpDir); err != nil {
		t.Fatalf("run (mixed archives) failed: %v", err)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "fireemblem.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "mariokart.nds"))
	checkFileNotExists(t, filepath.Join(tmpDir, "gba_games.zip"))
	checkFileNotExists(t, filepath.Join(tmpDir, "nds_games.rar"))
	checkFileExists(t, filepath.Join(tmpDir, "ignored.7z"))
}

func TestRun_NoArchivesFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rom-sorter-noarch")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "game.7z"), []byte("unsupported"), 0644)

	if err := run(tmpDir); err != nil {
		t.Errorf("run with no supported archives should not error, got: %v", err)
	}
}

// --- RAR Test Helpers ---

// createTestRar builds a minimal RAR v4 archive (store mode) with the given files.
func createTestRar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test rar: %v", err)
	}
	defer out.Close()

	// RAR v4 marker: "Rar!\x1A\x07\x00" (version 0 = 1.5-4.x format)
	out.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00})

	// Archive header (type 0x73)
	writeRarBlock(out, 0x73, 0x0000, nil)

	for name, content := range files {
		writeRarStoredFile(out, name, []byte(content))
	}

	// End of archive (type 0x7b)
	writeRarBlock(out, 0x7b, 0x0000, nil)
}

func createTestRarSlip(t *testing.T, path string, evilName string, content []byte) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create slip rar: %v", err)
	}
	defer out.Close()

	out.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00})
	writeRarBlock(out, 0x73, 0x0000, nil)
	writeRarStoredFile(out, evilName, content)
	writeRarBlock(out, 0x7b, 0x0000, nil)
}

// rarBlockHeader writes a RAR v4 block header with correct CRC-32 checksum.
// payload is the data after the 7-byte preamble (type, flags, size excluded).
func writeRarBlock(out *os.File, blockType byte, flags uint16, payload []byte) {
	// Total header size = 7 (preamble) + len(payload)
	headerSize := uint16(7 + len(payload))

	// Build the header data starting from type byte (for CRC computation)
	// Type(1) + Flags(2) + Size(2) + Payload
	crcData := make([]byte, 0, 5+len(payload))
	crcData = append(crcData, blockType)
	crcData = append(crcData, byte(flags), byte(flags>>8))
	crcData = append(crcData, byte(headerSize), byte(headerSize>>8))
	crcData = append(crcData, payload...)

	// Compute CRC-32-IEEE and take bottom 16 bits
	crc := crc32.ChecksumIEEE(crcData)
	crc16 := uint16(crc) // lower 16 bits

	// Write preamble: CRC(2) + Type(1) + Flags(2) + Size(2)
	out.Write([]byte{byte(crc16), byte(crc16 >> 8), blockType, byte(flags), byte(flags >> 8), byte(headerSize), byte(headerSize >> 8)})
	// Write payload
	out.Write(payload)
}

// writeRarStoredFile writes a stored (uncompressed) RAR v4 file entry.
func writeRarStoredFile(out *os.File, name string, data []byte) {
	nameBytes := []byte(name)

	// Build payload: PackedSize(4) + UnpackedSize(4) + HostOS(1) + FileCRC(4) +
	//   DOS time(4) + UnpackVer(1) + Method(1) + NameSize(2) + Attributes(4) + Name
	payload := make([]byte, 0, 25+len(nameBytes))

	putLE32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

	// Packed size (stored = uncompressed)
	payload = append(payload, putLE32(uint32(len(data)))...)
	// Unpacked size
	payload = append(payload, putLE32(uint32(len(data)))...)
	// Host OS: 2 = Windows (stored as value-1, so write 1)
	payload = append(payload, 1)
	// File CRC-32
	fcrc := crc32.ChecksumIEEE(data)
	payload = append(payload, putLE32(fcrc)...)
	// DOS file time: 2024-01-01 12:00:00
	// DOS time: bits 15-11=hour, 10-5=minute, 4-0=second/2
	// DOS date: bits 15-9=year-1980, 8-5=month, 4-0=day
	// 2024-01-01: date = (44<<9)|(1<<5)|1 = 0x5821; time = (12<<11)|0|0 = 0x6000
	payload = append(payload, putLE32(0x58216000)...)
	// Unpack version needed: 29 (RAR 2.9)
	payload = append(payload, 29)
	// Method: 0x30 = stored
	payload = append(payload, 0x30)
	// Name size
	payload = append(payload, byte(len(nameBytes)), byte(len(nameBytes)>>8))
	// File attributes: 0x20 = archive
	payload = append(payload, putLE32(0x20)...)
	// Filename
	payload = append(payload, nameBytes...)

	// Write file block header with blockHasData flag (0x8000)
	writeRarBlock(out, 0x74, 0x8000, payload)

	// Write file data immediately after
	out.Write(data)
}

// --- Helpers ---

func createTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		writer, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %q: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
}

func checkFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %q to exist", path)
	}
}

func checkFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file %q to not exist", path)
	}
}
