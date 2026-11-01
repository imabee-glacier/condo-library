package condo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedFromFileDeletesSource(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	src := filepath.Join(dir, "condo-pass.txt")
	if err := os.WriteFile(src, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(storeDir)
	if err := store.SeedFromFile("user@example.com", src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should be gone after seeding")
	}
	creds, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if creds.Username != "user@example.com" || creds.Password != "s3cret" {
		t.Fatal("seeded credential round trip mismatch")
	}
	creds.Clear()
}

func TestSeedFromFileRefusesOpenPermissions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "condo-pass.txt")
	if err := os.WriteFile(src, []byte("s3cret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(filepath.Join(dir, "store"))
	if err := store.SeedFromFile("user@example.com", src); err == nil {
		t.Fatal("expected refusal for open permissions")
	}
	// Refused seeding leaves the file untouched, so the user can fix it.
	if _, err := os.Stat(src); err != nil {
		t.Fatal("refused seeding should leave the source file untouched")
	}
}

func TestSeedFromFileRefusesGitRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "condo-pass.txt")
	if err := os.WriteFile(src, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(filepath.Join(dir, "store"))
	if err := store.SeedFromFile("user@example.com", src); err == nil {
		t.Fatal("expected refusal inside a git repository")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("refused seeding should leave the source file untouched")
	}
}

func TestSeedFromFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(filepath.Join(dir, "store"))
	if err := store.SeedFromFile("user@example.com", link); err == nil {
		t.Fatal("expected refusal for a symlink source")
	}
}

func TestSecureDelete(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "del.txt")
	if err := os.WriteFile(src, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SecureDelete(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("file should be verified gone after secure delete")
	}
}
