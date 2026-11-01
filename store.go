package condo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credential store.
//
// Secure Vault limitation, stated plainly: Muse's Secure Vault does NOT
// hand a stored password back to an SDK, to Muse, or to any tool. It only
// fills the password in the browser login flow. Therefore this SDK cannot
// read a password out of the Secure Vault, and no code here tries to.
//
// For autonomous, browser-free operation the SDK instead uses its own
// encrypted store, enrolled once by the user in a separate one-time step
// (see the setup-auth CLI command). That step is NOT done by copying from
// the Secure Vault, which is impossible by design, and must NOT ask for
// the password in chat. The password is read securely from stdin without
// echo by setup-auth, encrypted immediately, and never stored in plain
// text.
//
// Store layout, both files 0600, directory 0700:
//   - credentials.key : 32 random bytes, the AES-256 key, kept separate
//     from the ciphertext.
//   - credentials.enc : nonce (12 bytes, GCM standard) + AES-256-GCM
//     ciphertext of a JSON object {username, password}.
//
// Plaintext exists only in memory. Errors never include credential values.
// This store is not a substitute for a password manager; it is the
// mechanism that lets scheduled, browser-free checks run without the user
// re-entering a password on every run, after the one-time setup.

const (
	credentialKeyFile = "credentials.key"
	credentialEncFile = "credentials.enc"
)

// CredentialStore is a directory holding an encrypted credential pair.
type CredentialStore struct {
	Dir string
}

// NewCredentialStore returns a store rooted at dir. It creates nothing.
func NewCredentialStore(dir string) CredentialStore { return CredentialStore{Dir: dir} }

// Exists reports whether both store files are present.
func (s CredentialStore) Exists() bool {
	_, kerr := os.Stat(filepath.Join(s.Dir, credentialKeyFile))
	_, eerr := os.Stat(filepath.Join(s.Dir, credentialEncFile))
	return kerr == nil && eerr == nil
}

func writePrivateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Write to a temporary file in the same directory, then rename, so a
	// partial write never leaves a truncated credential file in place.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Enroll encrypts username and password into the store. Password is a
// byte slice so the caller can zero it after the call; this function does
// not retain it. Enrolling overwrites any previous store files.
func (s CredentialStore) Enroll(username string, password []byte) error {
	if username == "" || len(password) == 0 {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", fmt.Errorf("username and password are required"))
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	plain, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{Username: username, Password: string(password)})
	if err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plain, nil)
	// Zero the marshalled plaintext copy as soon as it is encrypted.
	for i := range plain {
		plain[i] = 0
	}
	if err := writePrivateFile(filepath.Join(s.Dir, credentialKeyFile), key); err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	if err := writePrivateFile(filepath.Join(s.Dir, credentialEncFile), ciphertext); err != nil {
		return wrapErr(ErrCodeValidation, "CredentialStore.Enroll", err)
	}
	for i := range key {
		key[i] = 0
	}
	return nil
}

// Load decrypts and returns credentials. The caller owns the returned
// values and should call Clear when finished.
func (s CredentialStore) Load() (*Credentials, error) {
	key, err := os.ReadFile(filepath.Join(s.Dir, credentialKeyFile))
	if err != nil {
		return nil, wrapErr(ErrCodeUnauthenticated, "CredentialStore.Load", fmt.Errorf("credential store key is missing; run setup-auth once"))
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	ciphertext, err := os.ReadFile(filepath.Join(s.Dir, credentialEncFile))
	if err != nil {
		return nil, wrapErr(ErrCodeUnauthenticated, "CredentialStore.Load", fmt.Errorf("credential store is missing; run setup-auth once"))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, wrapErr(ErrCodeValidation, "CredentialStore.Load", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, wrapErr(ErrCodeValidation, "CredentialStore.Load", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, wrapErr(ErrCodeUnauthenticated, "CredentialStore.Load", fmt.Errorf("credential store is corrupt"))
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, wrapErr(ErrCodeUnauthenticated, "CredentialStore.Load", fmt.Errorf("credential store could not be decrypted"))
	}
	var parsed struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(plain, &parsed); err != nil {
		return nil, wrapErr(ErrCodeValidation, "CredentialStore.Load", err)
	}
	for i := range plain {
		plain[i] = 0
	}
	return &Credentials{Username: parsed.Username, Password: parsed.Password}, nil
}

// ClientFromStore builds a client whose in-memory credentials come from
// the encrypted store. It does not authenticate yet; call Authenticate
// or let a read re-authenticate on a 401/403.
func ClientFromStore(baseURL string, store CredentialStore) (*Client, *Credentials, error) {
	creds, err := store.Load()
	if err != nil {
		return nil, nil, err
	}
	client, err := NewClient(Config{BaseURL: baseURL, Credentials: creds})
	if err != nil {
		creds.Clear()
		return nil, nil, err
	}
	return client, creds, nil
}
