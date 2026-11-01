package condo

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Secure temp-file seeding.
//
// The setup-auth command accepts --from-file <path> as an alternative to
// reading a password from stdin without echo. The temp file is a
// user-created, one-time carrier only:
//
//   - It must live outside any git repository, must not be a symlink,
//     and must have permissions no more open than 0600. Otherwise
//     seeding is refused and the file is left untouched, so the user
//     can fix the location or permissions and retry.
//   - It is read once, in memory only. Its contents are never printed,
//     logged, returned, copied to chat, to memory, or to any other file.
//   - It is encrypted immediately into the credential store (AES-256-GCM,
//     separate key file, both 0600).
//   - Only after successful encryption, the source file is securely
//     deleted in the same run: overwritten with random bytes, unlinked,
//     and verified gone. The command reports only success and deletion
//     verification, never the password or its length.
//
// If seeding fails before encryption, the source file is left untouched
// (so a failed attempt does not destroy the only copy). If encryption
// succeeds but secure deletion fails, an error is returned and the
// caller must treat the source file as still present and sensitive.

// isInsideGitRepo walks up from dir looking for a .git entry.
func isInsideGitRepo(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

// SecureDelete overwrites a file with random bytes of the same size,
// unlinks it, and verifies it is gone. It returns an error if any step
// fails or if verification shows the file still exists.
func SecureDelete(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return wrapErr(ErrCodeValidation, "SecureDelete", fmt.Errorf("source file is not accessible"))
	}
	if info.IsDir() {
		return wrapErr(ErrCodeValidation, "SecureDelete", fmt.Errorf("source file is a directory"))
	}
	size := info.Size()
	if size > 0 {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return wrapErr(ErrCodeValidation, "SecureDelete", err)
		}
		random := make([]byte, size)
		if _, err := rand.Read(random); err != nil {
			f.Close()
			return wrapErr(ErrCodeValidation, "SecureDelete", err)
		}
		if _, err := f.WriteAt(random, 0); err != nil {
			f.Close()
			return wrapErr(ErrCodeValidation, "SecureDelete", err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return wrapErr(ErrCodeValidation, "SecureDelete", err)
		}
		if err := f.Close(); err != nil {
			return wrapErr(ErrCodeValidation, "SecureDelete", err)
		}
		for i := range random {
			random[i] = 0
		}
	}
	if err := os.Remove(path); err != nil {
		return wrapErr(ErrCodeValidation, "SecureDelete", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return wrapErr(ErrCodeValidation, "SecureDelete", fmt.Errorf("source file could not be verified as deleted"))
	}
	return nil
}

// SeedFromFile enrolls credentials from a temp file, then securely
// deletes that file in the same run. The file must contain only the
// password; a single trailing newline is stripped. The username is
// supplied separately by the caller, not read from the file.
func (s CredentialStore) SeedFromFile(username, path string) error {
	const op = "CredentialStore.SeedFromFile"
	if strings.TrimSpace(username) == "" {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("username is required"))
	}
	if strings.TrimSpace(path) == "" {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file path is required"))
	}
	info, err := os.Lstat(path)
	if err != nil {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file is not accessible"))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file must not be a symlink"))
	}
	if info.IsDir() {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file is a directory"))
	}
	// Refuse overly open permissions: no group or other access at all.
	if info.Mode().Perm()&0o077 != 0 {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file permissions are too open; create it with 0600 outside any repository"))
	}
	if isInsideGitRepo(filepath.Dir(path)) {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file must live outside any git repository"))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file could not be read"))
	}
	// Strip only a single trailing newline pair, preserving any other
	// characters in the password exactly.
	pw := raw
	if len(pw) > 0 && pw[len(pw)-1] == '\n' {
		pw = pw[:len(pw)-1]
	}
	if len(pw) > 0 && pw[len(pw)-1] == '\r' {
		pw = pw[:len(pw)-1]
	}
	if len(pw) == 0 {
		for i := range raw {
			raw[i] = 0
		}
		return wrapErr(ErrCodeValidation, op, fmt.Errorf("source file contains no password"))
	}
	// Encrypt immediately. Enroll overwrites any previous store.
	if err := s.Enroll(username, pw); err != nil {
		for i := range raw {
			raw[i] = 0
		}
		for i := range pw {
			pw[i] = 0
		}
		return err
	}
	for i := range raw {
		raw[i] = 0
	}
	for i := range pw {
		pw[i] = 0
	}
	// Only after successful encryption, securely delete the source.
	if err := SecureDelete(path); err != nil {
		return err
	}
	return nil
}
