package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileUpdatablePaths(t *testing.T) {
	got := profileUpdatablePaths([]string{"nickname", "roleIds", "role_ids", "status", "orgUnitIds", "tenantId", "realname", "id"})
	require.Equal(t, []string{"nickname", "realname", "id"}, got)

	all := profileUpdatablePaths(nil)
	require.NotContains(t, all, "role_ids")
	require.NotContains(t, all, "status")
	require.Contains(t, all, "nickname")
}
