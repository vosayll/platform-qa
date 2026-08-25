package stands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"locali-e2e-engine/pkg/client"
)

const vaultsDirName = "vaults"

// VaultsDir returns the directory where per-stand vault snapshots are kept:
// <dataDir>/vaults/<standID>.json
func VaultsDir(dataDir string) string {
	return filepath.Join(dataDir, vaultsDirName)
}

// vaultPath resolves the snapshot file for standID, rejecting malformed ids.
func vaultPath(dataDir, standID string) (string, error) {
	if standID == "" || standID == "." || standID == ".." || strings.ContainsAny(standID, `/\`) {
		return "", fmt.Errorf("stands vault: invalid stand id %q", standID)
	}
	return filepath.Join(VaultsDir(dataDir), standID+".json"), nil
}

// SaveVault atomically persists the vault snapshot of standID (tmp file + rename).
func SaveVault(dataDir, standID string, snap *client.VaultSnapshot) error {
	path, err := vaultPath(dataDir, standID)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("stands vault: mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("stands vault: marshal stand %s: %w", standID, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("stands vault: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("stands vault: rename: %w", err)
	}
	return nil
}

// LoadVault reads the persisted snapshot of standID. ok is false when no
// snapshot has been stored yet (missing file is not an error).
func LoadVault(dataDir, standID string) (snap *client.VaultSnapshot, ok bool, err error) {
	path, err := vaultPath(dataDir, standID)
	if err != nil {
		return nil, false, err
	}

	data, rerr := os.ReadFile(path)
	switch {
	case os.IsNotExist(rerr):
		return nil, false, nil
	case rerr != nil:
		return nil, false, fmt.Errorf("stands vault: read %s: %w", path, rerr)
	}

	snap = &client.VaultSnapshot{}
	if uerr := json.Unmarshal(data, snap); uerr != nil {
		return nil, false, fmt.Errorf("stands vault: corrupt %s: %w", path, uerr)
	}
	return snap, true, nil
}

// RemoveVault deletes the snapshot file of standID. Errors are deliberately
// ignored: a stale vault file must never block stand deletion.
func RemoveVault(dataDir, standID string) {
	if path, err := vaultPath(dataDir, standID); err == nil {
		_ = os.Remove(path)
	}
}
