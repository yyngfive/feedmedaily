// Package backups publishes self-contained work-data snapshots, never credentials.
package backups

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

type Entry struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
}
type FileInfo struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Manifest struct {
	FormatVersion int        `json:"format_version"`
	AppVersion    string     `json:"app_version"`
	CreatedAt     time.Time  `json:"created_at"`
	Files         []FileInfo `json:"files"`
	Missing       []string   `json:"missing"`
}

const RestoreInstructions = `# Restore FeedMeDaily work data

Use the same FeedMeDaily version recorded in manifest.json. This is not a
cross-version downgrade tool. App settings, model credentials, and API keys
are not included; keep or re-enter them on the destination machine.

1. Download and extract the ZIP into a separate empty folder. Check the SHA-256
   hashes in manifest.json (PowerShell: Get-FileHash -Algorithm SHA256 <file>).
2. Locate the destination data directory in Settings > App > Local app.
   Source mode normally uses <project>/data; installed mode uses
   %LOCALAPPDATA%/FeedMeDaily/data. Use the directory shown by your app.
3. Exit FeedMeDaily from the tray and stop every backend using that directory.
   Do not restore while any app, background job, or SQLite connection is open.
4. Move the existing literature.sqlite, literature.sqlite-wal,
   literature.sqlite-shm, classification_profile.json and rss_feeds.json to a
   separate safety folder. Never combine an old WAL/SHM with the restored DB.
5. Copy literature.sqlite and the included JSON files into the data directory.
   If manifest.json lists a JSON file as missing, leave that file absent in the
   destination; do not retain the destination's old profile or subscriptions.
6. Start the same app version. Check paper counts, classifications, feedback,
   profile and subscriptions before starting sync. Reports are rebuilt from
   the restored database; logs and report caches are not part of this backup.

Keep the ZIP private: articles and feedback are personal work data, and saved
subscription URLs can contain access parameters. The ZIP is not encrypted.
`

var validID = regexp.MustCompile(`^work-data-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[a-zA-Z0-9]+\.zip$`)

func Directory(settings config.Settings) string { return filepath.Join(settings.DataDir, "backups") }

func List(settings config.Settings) ([]Entry, error) {
	entries, err := os.ReadDir(Directory(settings))
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, entry := range entries {
		if !validID.MatchString(entry.Name()) || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		created, err := time.Parse("20060102T150405.000000000Z", entry.Name()[10:36])
		if err != nil {
			continue
		}
		result = append(result, Entry{ID: entry.Name(), CreatedAt: created, Size: info.Size()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func Path(settings config.Settings, id string) (string, error) {
	if !validID.MatchString(id) {
		return "", os.ErrNotExist
	}
	path := filepath.Join(Directory(settings), id)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

func Create(ctx context.Context, settings config.Settings, version string, configLock sync.Locker, progress func(string)) (Entry, error) {
	report := func(stage string) {
		if progress != nil {
			progress(stage)
		}
	}
	report("Preparing backup")
	directory := Directory(settings)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return Entry{}, err
	}
	temp, err := os.MkdirTemp(directory, ".work-data-")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(temp) // Generated child of the configured backup directory only.
	manifest := Manifest{FormatVersion: 1, AppVersion: version, CreatedAt: time.Now().UTC(), Files: []FileInfo{}, Missing: []string{}}
	err = func() error {
		configLock.Lock()
		defer configLock.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(settings.DatabasePath); err != nil {
			return err
		}
		report("Creating database snapshot")
		db, err := store.OpenRead(settings.DatabasePath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.BackupToContext(ctx, filepath.Join(temp, "literature.sqlite")); err != nil {
			return err
		}
		for _, file := range []struct{ name, path string }{{"classification_profile.json", settings.ProfilePath}, {"rss_feeds.json", settings.FeedsPath}} {
			data, err := os.ReadFile(file.path)
			if errors.Is(err, os.ErrNotExist) {
				manifest.Missing = append(manifest.Missing, file.name)
				continue
			}
			if err != nil {
				return err
			}
			if !json.Valid(data) {
				return fmt.Errorf("%s is not valid JSON", file.name)
			}
			if err := os.WriteFile(filepath.Join(temp, file.name), data, 0600); err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		return Entry{}, err
	}
	snapshot, err := store.OpenRead(filepath.Join(temp, "literature.sqlite"))
	if err != nil {
		return Entry{}, err
	}
	err = snapshot.QuickCheck(ctx)
	snapshot.Close()
	if err != nil {
		return Entry{}, err
	}
	if err := os.WriteFile(filepath.Join(temp, "RESTORE.md"), []byte(RestoreInstructions), 0600); err != nil {
		return Entry{}, err
	}
	files, err := os.ReadDir(temp)
	if err != nil {
		return Entry{}, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return Entry{}, err
		}
		// SQLite may create WAL/SHM sidecars on opening the snapshot: never archive them.
		if file.Name() != "literature.sqlite" && file.Name() != "classification_profile.json" && file.Name() != "rss_feeds.json" && file.Name() != "RESTORE.md" {
			continue
		}
		f, err := os.Open(filepath.Join(temp, file.Name()))
		if err != nil {
			return Entry{}, err
		}
		hash := sha256.New()
		size, err := io.Copy(hash, f)
		f.Close()
		if err != nil {
			return Entry{}, err
		}
		manifest.Files = append(manifest.Files, FileInfo{Name: file.Name(), SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Entry{}, err
	}
	if err := os.WriteFile(filepath.Join(temp, "manifest.json"), data, 0600); err != nil {
		return Entry{}, err
	}
	report("Packaging backup")
	archivePath := filepath.Join(temp, "archive.zip")
	if err := writeArchive(ctx, archivePath, temp, manifest); err != nil {
		return Entry{}, err
	}
	// Read every member to EOF, checking CRC and matching the recorded hashes.
	if err := validateArchive(ctx, archivePath, manifest); err != nil {
		return Entry{}, err
	}
	id := "work-data-" + manifest.CreatedAt.Format("20060102T150405.000000000Z") + "-" + filepath.Base(temp)[len(".work-data-"):] + ".zip"
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(archivePath, filepath.Join(directory, id)); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(filepath.Join(directory, id))
	if err != nil {
		return Entry{}, err
	}
	report("Backup complete")
	return Entry{ID: id, CreatedAt: manifest.CreatedAt, Size: info.Size()}, nil
}

func writeArchive(ctx context.Context, path, dir string, m Manifest) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	writer := zip.NewWriter(file)
	names := []string{"manifest.json"}
	for _, f := range m.Files {
		names = append(names, f.Name)
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			writer.Close()
			return err
		}
		input, e := os.Open(filepath.Join(dir, name))
		if e != nil {
			writer.Close()
			return e
		}
		output, e := writer.Create(name)
		if e == nil {
			_, e = io.Copy(output, input)
		}
		input.Close()
		if e != nil {
			writer.Close()
			return e
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return file.Sync()
}

func validateArchive(ctx context.Context, path string, m Manifest) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	expected := map[string]FileInfo{}
	for _, f := range m.Files {
		expected[f.Name] = f
	}
	if len(archive.File) != len(expected)+1 {
		return fmt.Errorf("incomplete backup archive")
	}
	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		hash := sha256.New()
		size, err := io.Copy(hash, input)
		input.Close()
		if err != nil {
			return err
		}
		if file.Name == "manifest.json" {
			continue
		}
		want, ok := expected[file.Name]
		if !ok || size != want.Size || hex.EncodeToString(hash.Sum(nil)) != want.SHA256 {
			return fmt.Errorf("backup checksum mismatch: %s", file.Name)
		}
	}
	return nil
}
