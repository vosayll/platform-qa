package stands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"locali-e2e-engine/pkg/client"
)

func newTestSessionMgr() *client.SessionManager {
	return client.NewSessionManager(client.NewHTTPClient("http://localhost:0", time.Second))
}

func vaultClientCount(t *testing.T, sm *client.SessionManager) int {
	t.Helper()
	vault := sm.GetVault()
	clients, ok := vault["clients"].([]*client.TokenProfile)
	require.True(t, ok)
	return len(clients)
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	src := newTestSessionMgr()
	c1 := src.AddToken("client", "C1", "+79990000001", "tok-client-1", true)
	c2 := src.AddToken("client", "C2", "+79990000002", "tok-client-2", false)
	r1 := src.AddToken("rest", "R1", "rest1", "tok-rest-1", true)
	k1 := src.AddToken("courier", "K1", "+79990000003", "tok-courier-1", true)
	a1 := src.AddToken("admin", "A1", "admin1", "tok-admin-1", true)

	require.True(t, src.HasToken(c2.Token))

	snap := src.Snapshot()
	require.Len(t, snap.Profiles, 5)
	require.Equal(t, c1.ID, snap.Active["client"])
	require.Equal(t, r1.ID, snap.Active["rest"])
	require.Equal(t, k1.ID, snap.Active["courier"])
	require.Equal(t, a1.ID, snap.Active["admin"])

	// Mutate the source after snapshotting: the copy must stay detached.
	src.DeleteProfile("client", c1.ID)
	src.AddToken("client", "C3", "+79990000004", "tok-client-3", true)

	dst := newTestSessionMgr()
	dst.Restore(snap)

	vault := dst.GetVault()
	require.Len(t, vault["clients"].([]*client.TokenProfile), 2)
	require.Len(t, vault["rests"].([]*client.TokenProfile), 1)
	require.Len(t, vault["couriers"].([]*client.TokenProfile), 1)
	require.Len(t, vault["admins"].([]*client.TokenProfile), 1)

	got := dst.GetClientSession()
	require.Equal(t, c1.Token, got.Token)
	require.Equal(t, "+79990000001", got.Identifier)
	require.Equal(t, dst.GetRestSession().Token, r1.Token)
	require.Equal(t, dst.GetCourierSession().Token, k1.Token)
	require.Equal(t, dst.GetAdminSession().Token, a1.Token)

	// Active pointer must follow the snapshot, not pool order.
	require.Equal(t, c1.ID, dst.GetClientSession().ID)
	active := dst.GetVault()["activeClientId"].(string)
	require.Equal(t, c1.ID, active)

	// Role aliases normalize like AddToken does.
	snapAlias := &client.VaultSnapshot{
		Profiles: []*client.TokenProfile{
			{ID: "x1", Role: "restaurant", Token: "tok-alias"},
		},
	}
	dst.Restore(snapAlias)
	require.Len(t, dst.GetVault()["rests"].([]*client.TokenProfile), 1)
}

func TestRestoreNilAndInvalidActive(t *testing.T) {
	sm := newTestSessionMgr()
	sm.AddToken("client", "C1", "+79990000001", "tok-a", true)
	sm.AddToken("admin", "A1", "admin", "tok-b", true)

	sm.Restore(nil)
	require.Zero(t, vaultClientCount(t, sm))
	require.Empty(t, sm.GetClientSession().Token)
	require.Empty(t, sm.GetAdminSession().Token)

	// Empty snapshot -> empty storage.
	sm.Restore(&client.VaultSnapshot{})
	require.Zero(t, vaultClientCount(t, sm))

	// Active pointer to a non-existent profile -> role without an active.
	p := sm.AddToken("client", "C2", "+79990000009", "tok-c", false)
	sm.Restore(&client.VaultSnapshot{
		Profiles: []*client.TokenProfile{p},
		Active:   map[string]string{"client": "no-such-id"},
	})
	require.Len(t, sm.GetVault()["clients"].([]*client.TokenProfile), 1)
	require.Empty(t, sm.GetClientSession().Token)
}

func TestSaveLoadRemoveVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()

	sm := newTestSessionMgr()
	c1 := sm.AddToken("client", "C1", "+79990000001", "tok-persist", true)

	require.NoError(t, SaveVault(dir, "stand-a", sm.Snapshot()))

	path := filepath.Join(dir, "vaults", "stand-a.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "tok-persist")

	loaded, ok, err := LoadVault(dir, "stand-a")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, loaded)
	require.Equal(t, c1.ID, loaded.Active["client"])

	other := newTestSessionMgr()
	other.Restore(loaded)
	require.Equal(t, "tok-persist", other.GetClientSession().Token)

	_, ok, err = LoadVault(dir, "missing-stand")
	require.NoError(t, err)
	require.False(t, ok)

	// Atomic write must not leak tmp files.
	entries, err := os.ReadDir(filepath.Join(dir, "vaults"))
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp")
	}

	RemoveVault(dir, "stand-a")
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
	RemoveVault(dir, "stand-a") // idempotent
}

func TestSaveVaultRejectsBadStandID(t *testing.T) {
	dir := t.TempDir()
	require.Error(t, SaveVault(dir, "", nil))
	require.Error(t, SaveVault(dir, "../escape", nil))
	require.Error(t, SaveVault(dir, "a/b", nil))
	require.NoFileExists(t, filepath.Join(dir, "escape.json"))
}
