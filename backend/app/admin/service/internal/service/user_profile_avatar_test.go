package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/ossutil"
)

// 换头像时只删本人头像目录下、由本服务上传的旧对象。
func TestOwnedAvatarObject(t *testing.T) {
	const host = "http://minio:9000"
	own := ossutil.JoinObjectUrl(host, "images", "avatar/7/abc.png")

	bucket, object, ok := ownedAvatarObject(host, own, 7)
	require.True(t, ok)
	require.Equal(t, "images", bucket)
	require.Equal(t, "avatar/7/abc.png", object)

	for name, url := range map[string]string{
		"其他用户的头像": ossutil.JoinObjectUrl(host, "images", "avatar/8/abc.png"),
		"前缀相似的用户": ossutil.JoinObjectUrl(host, "images", "avatar/77/abc.png"),
		"普通图片文件":  ossutil.JoinObjectUrl(host, "images", "temp/abc.png"),
		"其他桶":     ossutil.JoinObjectUrl(host, "files", "avatar/7/abc.png"),
		"外链头像":    "https://example.com/images/avatar/7/abc.png",
		"子目录穿越":   ossutil.JoinObjectUrl(host, "images", "avatar/7/../8/abc.png"),
		"带查询串":    own + "?x=1",
		"旧版无目录头像": ossutil.JoinObjectUrl(host, "images", "abc.png"),
		"空 URL":   "",
	} {
		_, _, ok := ownedAvatarObject(host, url, 7)
		require.False(t, ok, name)
	}
}
