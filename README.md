# ROM Sorter

Extracts and sorts ROMs from `.zip` and `.rar` archives into console-specific folders.

## Supported Archives

- `.zip` — standard ZIP archives
- `.rar` — RAR v4/v5 archives (via `nwaples/rardecode`)

## Supported ROM Types

| Extension | Target Folder |
|-----------|--------------|
| `.gba`, `.gb`, `.gbc` | `GBA/` |
| `.nds` | `NDS/` |
| `.3ds`, `.cia` | `3DS/` |

## Usage

```powershell
# Process current directory  
.\rom-sorter.exe

# Process specific directory
.\rom-sorter.exe "D:\My ROMs"
```

The tool will:
1. Scan the directory for `.zip` and `.rar` files
2. Extract each archive to a temp directory
3. Move ROM files to their target folders
4. Delete the source archive (non-ROM content is discarded)

## Build

```powershell
go build -buildvcs=false -o rom-sorter.exe .
```

## Test

```powershell
go test ./...
```

## Security

- Zip-slip / RAR-slip path traversal prevented
- Temp extraction with 0700 permissions
- Size limits: 4GB per file, 8GB total, 100K entries max
- Symlinks skipped, directory traversal blocked
