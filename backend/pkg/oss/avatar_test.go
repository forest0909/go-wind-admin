package oss

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAvatarURL(t *testing.T) {
	uid, object, ok := ParseAvatarURL(AvatarURL(7, "abc.png"))
	require.True(t, ok)
	require.Equal(t, uint32(7), uid)
	require.Equal(t, "avatar/7/abc.png", object)
	require.Equal(t, "/admin/v1/avatars/7/abc.png", AvatarURL(7, "abc.png"))

	for _, u := range []string{
		"",
		"/admin/v1/avatars/7",
		"/admin/v1/avatars/7/",
		"/admin/v1/avatars//abc.png",
		"/admin/v1/avatars/x/abc.png",
		"/admin/v1/avatars/07/abc.png",
		"/admin/v1/avatars/-1/abc.png",
		"/admin/v1/avatars/99999999999/abc.png",
		"/admin/v1/avatars/7/a/b.png",
		"/admin/v1/avatars/7/..",
		"/admin/v1/avatars/7/.hidden",
		"/admin/v1/avatars/7/../../files/x.png",
		`/admin/v1/avatars/7/a\b.png`,
		"/admin/v1/avatars/7/abc.png?x=1",
		"https://example.com/admin/v1/avatars/7/abc.png",
		"/admin/v1/file/image?path=/images/avatar/7/abc.png",
	} {
		_, _, ok := ParseAvatarURL(u)
		require.False(t, ok, u)
	}
}

func TestLegacyAvatarObject(t *testing.T) {
	const host = "minio:9000"
	object, ok := LegacyAvatarObject(host, "minio:9000/images/avatar/7/abc.png", 7)
	require.True(t, ok)
	require.Equal(t, "avatar/7/abc.png", object)

	for _, u := range []string{
		"minio:9000/images/avatar/8/abc.png",
		"minio:9000/images/avatar/77/abc.png",
		"minio:9000/files/avatar/7/abc.png",
		"minio:9000/images/temp/abc.png",
		"minio:9000/images/avatar/7/sub/abc.png",
		"minio:9000/images/avatar/7/..",
		"https://example.com/images/avatar/7/abc.png",
		"other:9000/images/avatar/7/abc.png",
		"minio:9000/images/avatar/7/abc.png#x",
	} {
		_, ok := LegacyAvatarObject(host, u, 7)
		require.False(t, ok, u)
	}

	_, ok = LegacyAvatarObject("", "/images/avatar/7/abc.png", 7)
	require.False(t, ok, "empty host recognizes nothing")
}

func TestIsSafeInlineImage(t *testing.T) {
	for _, ct := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "IMAGE/PNG", "image/png; charset=binary"} {
		require.True(t, IsSafeInlineImage(ct), ct)
	}
	for _, ct := range []string{"image/svg+xml", "text/html", "text/plain; charset=utf-8", "image/bmp", "application/octet-stream", ""} {
		require.False(t, IsSafeInlineImage(ct), ct)
	}
}
