package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExe_StartsAndExitsCleanly(t *testing.T) {
	exePath := findExe(t)

	cmd := exec.Command(exePath, os.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed to start/exit cleanly: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "ROM Sorter") {
		t.Errorf("expected 'ROM Sorter' in output, got: %s", out)
	}
	if !strings.Contains(out, "Done") {
		t.Errorf("expected 'Done' in output, got: %s", out)
	}
}

func TestExe_NoArgsDoesNotCrash(t *testing.T) {
	exePath := findExe(t)

	// Running with no args should scan the exe's own directory,
	// show results, and wait for user input (press Enter)
	cmd := exec.Command(exePath)
	cmd.Stdin = strings.NewReader("\n") // simulate Enter press
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe crashed with no args: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "ROM Sorter") {
		t.Errorf("expected 'ROM Sorter' in output, got: %s", out)
	}
	if !strings.Contains(out, "Press Enter") {
		t.Errorf("expected 'Press Enter to exit' prompt in double-click mode, got: %s", out)
	}
}

func TestExe_ProcessesZipFilesE2E(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create target subdirectories
	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	// Create test zip with ROMs
	zipPath := filepath.Join(tmpDir, "e2e_test.zip")
	createE2ETestZip(t, zipPath, map[string]string{
		"zelda.gba":          "The Legend of Zelda ROM",
		"mario.nds":          "Super Mario ROM",
		"pokemon.3ds":        "Pokemon ROM",
		"readme.txt":         "Not a ROM",
	})

	// Run the exe against the temp directory
	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "e2e_test.zip") {
		t.Errorf("expected 'e2e_test.zip' in output, got: %s", out)
	}

	// Verify ROMs landed in correct folders
	checkFileExists(t, filepath.Join(tmpDir, "GBA", "zelda.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "mario.nds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "pokemon.3ds"))

	// Verify non-ROM not in ROM folders
	checkFileNotExists(t, filepath.Join(tmpDir, "GBA", "readme.txt"))

	// Verify zip was cleaned up
	checkFileNotExists(t, zipPath)
}

func TestExe_HandlesCorruptZipGracefully(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-corrupt")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a corrupt zip
	corruptZip := filepath.Join(tmpDir, "corrupt.zip")
	os.WriteFile(corruptZip, []byte("not a valid zip file"), 0644)

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// The exe should exit with non-zero on errors
		if !strings.Contains(string(output), "ERROR") {
			t.Errorf("expected ERROR in output for corrupt zip, got: %s", string(output))
		}
	}
}

func TestExe_IgnoresNonZipFiles(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-nonzip")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create non-zip files
	os.WriteFile(filepath.Join(tmpDir, "game.rar"), []byte("rar content"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("text"), 0644)

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if strings.Contains(out, "Processing:") {
		t.Errorf("should not process non-zip files, got: %s", out)
	}
}

func TestExe_NestedZipDirectories(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-nested")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	// Create a zip with ROMs in nested directories
	zipPath := filepath.Join(tmpDir, "nested.zip")
	createE2ETestZip(t, zipPath, map[string]string{
		"folder1/game1.gba": "gba1",
		"folder2/game2.nds": "nds1",
	})

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}
	t.Logf("output: %s", string(output))

	// ROMs should be extracted with just the filename (not full path)
	checkFileExists(t, filepath.Join(tmpDir, "GBA", "game1.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "game2.nds"))
}

func TestExe_DeduplicateConflictingROMs(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-dup")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gbaDir := filepath.Join(tmpDir, "GBA")
	os.MkdirAll(gbaDir, 0755)

	// Pre-create an existing ROM
	os.WriteFile(filepath.Join(gbaDir, "game.gba"), []byte("existing"), 0644)

	// Create zip with same ROM name
	zipPath := filepath.Join(tmpDir, "duplicate.zip")
	createE2ETestZip(t, zipPath, map[string]string{
		"game.gba": "new version",
	})

	cmd := exec.Command(exePath, tmpDir)
	_, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v", err)
	}

	// Both files should exist (original + deduplicated with timestamp)
	entries, _ := os.ReadDir(gbaDir)
	gbaCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".gba" {
			gbaCount++
		}
	}
	if gbaCount < 2 {
		t.Errorf("expected at least 2 .gba files after dedup, got %d", gbaCount)
	}
}

func TestExe_SummaryOnCompletion(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-summary")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "GBA"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "NDS"), 0755)

	// Create two zips
	createE2ETestZip(t, filepath.Join(tmpDir, "batch1.zip"), map[string]string{
		"game1.gba": "gba rom",
	})
	createE2ETestZip(t, filepath.Join(tmpDir, "batch2.zip"), map[string]string{
		"game2.nds": "nds rom",
	})

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	t.Logf("output: %s", out)

	// Should report number of zips processed
	if !strings.Contains(out, "Done") {
		t.Error("expected completion message")
	}
}

func TestExe_HandlesEmptyDirectory(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-empty")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed on empty dir: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "No .zip files found") {
		t.Errorf("expected 'No .zip files found' for empty dir, got: %s", out)
	}
}

func TestExe_ExitCodeOnErrors(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-exit")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a corrupt zip
	os.WriteFile(filepath.Join(tmpDir, "bad.zip"), []byte("not a zip"), 0644)

	cmd := exec.Command(exePath, tmpDir)
	err = cmd.Run()

	// Should have non-zero exit code when errors occur
	if err == nil {
		t.Error("exe should exit with non-zero code when errors occur")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() == 0 {
		t.Error("expected non-zero exit code for processing errors")
	}
}

func TestExe_TimesOutOnLargeZip(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-timeout")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "GBA"), 0755)

	// Create a reasonable zip
	createE2ETestZip(t, filepath.Join(tmpDir, "normal.zip"), map[string]string{
		"game.gba": "normal rom",
	})

	start := time.Now()
	cmd := exec.Command(exePath, tmpDir)
	err = cmd.Run()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("exe failed: %v", err)
	}

	// Should complete quickly for small zips
	if elapsed > 5*time.Second {
		t.Errorf("exe took too long: %v", elapsed)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "game.gba"))
	checkFileNotExists(t, filepath.Join(tmpDir, "normal.zip"))
}

// --- Helpers ---

func findExe(t *testing.T) string {
	t.Helper()

	candidates := []string{
		"rom-sorter.exe",
		filepath.Join("..", "rom-sorter.exe"),
		filepath.Join(filepath.Dir(os.Args[0]), "rom-sorter.exe"),
	}

	// Also search current working directory
	cwd, _ := os.Getwd()
	candidates = append(candidates, filepath.Join(cwd, "rom-sorter.exe"))

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}

	t.Fatal("rom-sorter.exe not found - build it first with: go build -o rom-sorter.exe .")
	return ""
}

func createE2ETestZip(t *testing.T, path string, files map[string]string) {
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

// --- E2E RAR Tests (Phase: RED — will fail until RAR support is implemented) ---

func TestExe_ProcessesRarFilesE2E(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-rar")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	rarPath := filepath.Join(tmpDir, "e2e_test.rar")
	createTestRar(t, rarPath, map[string]string{
		"zelda.gba":   "The Legend of Zelda ROM",
		"mario.nds":   "Super Mario ROM",
		"pokemon.3ds": "Pokemon ROM",
		"readme.txt":  "Not a ROM",
	})

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "e2e_test.rar") {
		t.Errorf("expected 'e2e_test.rar' in output, got: %s", out)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "zelda.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "mario.nds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "pokemon.3ds"))
	checkFileNotExists(t, filepath.Join(tmpDir, "GBA", "readme.txt"))
	checkFileNotExists(t, rarPath)
}

func TestExe_MixedZipAndRar(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-mixed")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	createE2ETestZip(t, filepath.Join(tmpDir, "gba.zip"), map[string]string{
		"zelda.gba": "zelda",
	})
	createTestRar(t, filepath.Join(tmpDir, "nds.rar"), map[string]string{
		"mario.nds": "mario",
	})

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	if !strings.Contains(out, "gba.zip") || !strings.Contains(out, "nds.rar") {
		t.Errorf("expected both archives in output, got: %s", out)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "zelda.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "mario.nds"))
	checkFileNotExists(t, filepath.Join(tmpDir, "gba.zip"))
	checkFileNotExists(t, filepath.Join(tmpDir, "nds.rar"))
}

func TestExe_HandlesCorruptRarGracefully(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-corruptrar")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "corrupt.rar"), []byte("not a valid rar"), 0644)

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "ERROR") {
			t.Errorf("expected ERROR in output for corrupt rar, got: %s", string(output))
		}
	}
}

func TestExe_SortsRarIntoCorrectFolders(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-rarsort")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, d := range []string{"GBA", "NDS", "3DS"} {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	rarPath := filepath.Join(tmpDir, "all.rar")
	createTestRar(t, rarPath, map[string]string{
		"advancewars.gba": "gba rom",
		"pokemon.nds":     "nds rom",
		"zelda.3ds":       "3ds rom",
		"fireemblem.cia":  "3ds rom",
	})

	cmd := exec.Command(exePath, tmpDir)
	_, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v", err)
	}

	checkFileExists(t, filepath.Join(tmpDir, "GBA", "advancewars.gba"))
	checkFileExists(t, filepath.Join(tmpDir, "NDS", "pokemon.nds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "zelda.3ds"))
	checkFileExists(t, filepath.Join(tmpDir, "3DS", "fireemblem.cia"))
	checkFileNotExists(t, rarPath)
}

func TestExe_RarDeduplication(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-rardedup")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gbaDir := filepath.Join(tmpDir, "GBA")
	os.MkdirAll(gbaDir, 0755)
	os.WriteFile(filepath.Join(gbaDir, "game.gba"), []byte("existing"), 0644)

	rarPath := filepath.Join(tmpDir, "dup.rar")
	createTestRar(t, rarPath, map[string]string{
		"game.gba": "new version",
	})

	cmd := exec.Command(exePath, tmpDir)
	_, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v", err)
	}

	entries, _ := os.ReadDir(gbaDir)
	gbaCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".gba" {
			gbaCount++
		}
	}
	if gbaCount < 2 {
		t.Errorf("expected at least 2 .gba files after rar dedup, got %d", gbaCount)
	}
	checkFileNotExists(t, rarPath)
}

func TestExe_UpdatedScanMessage(t *testing.T) {
	exePath := findExe(t)

	tmpDir, err := os.MkdirTemp("", "rom-sorter-e2e-scanmsg")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command(exePath, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exe failed: %v\noutput: %s", err, string(output))
	}

	out := string(output)
	// Updated message should mention archives, not just .zip
	if !strings.Contains(out, "Scanning") {
		t.Errorf("expected 'Scanning' in output, got: %s", out)
	}
	// Should not ONLY mention zip anymore
	if strings.Contains(out, ".zip files") && !strings.Contains(out, "archives") && !strings.Contains(out, ".rar") {
		t.Errorf("scan message should mention archives or .rar too, got: %s", out)
	}
}
