package oss

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/trans"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

func newLocalForTest(t *testing.T) (*LocalStorage, string) {
	t.Helper()
	root := t.TempDir()
	s, err := NewLocalStorage(root, bLogger.NopLogger())
	require.NoError(t, err)
	return s, root
}

func downloadReq(bucket, object string) *storageV1.DownloadFileRequest {
	return &storageV1.DownloadFileRequest{
		Selector: &storageV1.DownloadFileRequest_StorageObject{StorageObject: &storageV1.StorageObject{
			BucketName: trans.Ptr(bucket), ObjectName: trans.Ptr(object),
		}},
	}
}

// 上传落在 root/bucket/object，读、下载、删除都按同一路径；权限不是全局可写，不留临时文件。
func TestLocalStorage_UploadReadDownloadDelete(t *testing.T) {
	s, root := newLocalForTest(t)
	ctx := context.Background()
	body := []byte("hello local storage")

	info, storagePath, downloadURL, err := s.UploadFile(ctx, "files", "temp/a.txt", "text/plain", body)
	require.NoError(t, err)
	require.Equal(t, UploadInfo{Bucket: "files", Key: "temp/a.txt", Size: int64(len(body))}, info)
	require.Equal(t, "/files/temp/a.txt", storagePath)
	require.Empty(t, downloadURL)
	require.Equal(t, storageV1.OSSProvider_LOCAL, s.Provider())

	p := filepath.Join(root, "files", "temp", "a.txt")
	got, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, body, got)

	st, err := os.Stat(p)
	require.NoError(t, err)
	require.Zero(t, st.Mode().Perm()&0o002, "file must not be world-writable")
	dst, err := os.Stat(filepath.Dir(p))
	require.NoError(t, err)
	require.Zero(t, dst.Mode().Perm()&0o002, "dir must not be world-writable")

	entries, err := os.ReadDir(filepath.Dir(p))
	require.NoError(t, err)
	require.Len(t, entries, 1, "temp file must be renamed away")

	rc, ct, size, err := s.GetObjectReader(ctx, "files", "temp/a.txt")
	require.NoError(t, err)
	read, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, body, read)
	require.Contains(t, ct, "text/plain")
	require.Equal(t, int64(len(body)), size)

	resp, err := s.DownloadFile(ctx, downloadReq("files", "temp/a.txt"))
	require.NoError(t, err)
	require.Equal(t, body, resp.GetFile())
	require.Equal(t, int64(len(body)), resp.GetSize())

	// 预签名请求在本地存储下也直接返回内容
	presign := downloadReq("files", "temp/a.txt")
	presign.PreferPresignedUrl = trans.Ptr(true)
	resp, err = s.DownloadFile(ctx, presign)
	require.NoError(t, err)
	require.Equal(t, body, resp.GetFile())

	// Range 闭区间，与 MinIO 一致
	ranged := downloadReq("files", "temp/a.txt")
	ranged.RangeStart, ranged.RangeEnd = trans.Ptr(int64(0)), trans.Ptr(int64(4))
	resp, err = s.DownloadFile(ctx, ranged)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), resp.GetFile())

	// 覆盖写
	_, _, _, err = s.UploadFile(ctx, "files", "temp/a.txt", "text/plain", []byte("v2"))
	require.NoError(t, err)
	got, err = os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, []byte("v2"), got)

	require.NoError(t, s.DeleteFile(ctx, "files", "temp/a.txt"))
	_, err = os.Stat(p)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, s.DeleteFile(ctx, "files", "temp/a.txt"), "deleting a missing object is not an error")

	_, _, _, err = s.GetObjectReader(ctx, "files", "temp/a.txt")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = s.DownloadFile(ctx, downloadReq("files", "temp/a.txt"))
	require.True(t, storageV1.IsFileNotFound(err), "%v", err)
}

// Content-Type 按内容嗅探，不看扩展名：文本以 .html/.svg 命名不会被当成 HTML/SVG 返回。
func TestLocalStorage_ContentTypeByContentNotExtension(t *testing.T) {
	s, _ := newLocalForTest(t)
	ctx := context.Background()
	png := []byte("\x89PNG\r\n\x1a\nxxxx")
	text := []byte("hello<script>window.x=1</script>")
	cases := []struct {
		object string
		body   []byte
		want   string
	}{
		{"temp/payload.html", text, "text/plain"},
		{"temp/payload.svg", text, "text/plain"},
		{"temp/payload.htm", text, "text/plain"},
		{"avatar/1/fake.png", text, "text/plain"},
		{"temp/real.txt", png, "image/png"},
	}
	for _, c := range cases {
		_, _, _, err := s.UploadFile(ctx, "files", c.object, "", c.body)
		require.NoError(t, err)
		rc, ct, _, err := s.GetObjectReader(ctx, "files", c.object)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.True(t, strings.HasPrefix(ct, c.want), "%s: got %q", c.object, ct)

		resp, err := s.DownloadFile(ctx, downloadReq("files", c.object))
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(resp.GetMime(), c.want), "%s download: got %q", c.object, resp.GetMime())
	}
}

// root_dir 里的符号链接（中间目录或最终文件，指向根目录内外都算）一律拒绝，根目录外的文件不会被读、写、删。
func TestLocalStorage_RejectsSymlinks(t *testing.T) {
	s, root := newLocalForTest(t)
	ctx := context.Background()

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("secret"), 0o600))

	// 目录链接指向根目录外
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "evil")))
	// 最终文件链接指向根目录外
	require.NoError(t, os.MkdirAll(filepath.Join(root, "files", "d"), 0o750))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "files", "d", "link.txt")))
	// 根目录内的目录链接也拒绝
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join(root, "images"), filepath.Join(root, "files", "alias")))

	for _, c := range []struct{ bucket, object string }{
		{"evil", "secret.txt"},
		{"evil", "new.txt"},
		{"files", "d/link.txt"},
		{"files", "alias/x.png"},
	} {
		_, _, _, err := s.GetObjectReader(ctx, c.bucket, c.object)
		require.Error(t, err, "read %s/%s", c.bucket, c.object)
		_, err = s.DownloadFile(ctx, downloadReq(c.bucket, c.object))
		require.True(t, storageV1.IsBadRequest(err), "download %s/%s: %v", c.bucket, c.object, err)
		_, _, _, err = s.UploadFile(ctx, c.bucket, c.object, "text/plain", []byte("pwned"))
		require.True(t, storageV1.IsBadRequest(err), "upload %s/%s: %v", c.bucket, c.object, err)
		require.True(t, storageV1.IsBadRequest(s.DeleteFile(ctx, c.bucket, c.object)), "delete %s/%s", c.bucket, c.object)
	}

	got, err := os.ReadFile(secret)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), got, "outside file must be untouched")
	_, err = os.Stat(filepath.Join(outside, "new.txt"))
	require.ErrorIs(t, err, os.ErrNotExist, "nothing may be created outside root")
	entries, err := os.ReadDir(filepath.Join(root, "images"))
	require.NoError(t, err)
	require.Empty(t, entries)
}

// 根目录本身是符号链接（如 macOS 的 /tmp）可以用，按解析后的目录工作。
func TestNewLocalStorage_RootDirMayBeSymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "root-link")
	require.NoError(t, os.Symlink(real, link))
	s, err := NewLocalStorage(link, bLogger.NopLogger())
	require.NoError(t, err)
	_, _, _, err = s.UploadFile(context.Background(), "files", "a.txt", "text/plain", []byte("x"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(real, "files", "a.txt"))
	require.NoError(t, err)
}

// 目录穿越、绝对路径、空段一律拒绝，根目录外的文件不会被写、读、删。
func TestLocalStorage_RejectsPathTraversal(t *testing.T) {
	s, root := newLocalForTest(t)
	ctx := context.Background()

	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	t.Cleanup(func() { _ = os.Remove(outside) })

	cases := []struct{ bucket, object string }{
		{"files", "../../" + filepath.Base(outside)},
		{"..", filepath.Base(root) + "-outside.txt"},
		{"files", "a/../../x"},
		{"files", "/etc/passwd"},
		{"files", "a//b"},
		{"files", "./a"},
		{"files", "a/."},
		{"files", `a\..\..\x`},
		{"fi/les", "a"},
		{".", "a"},
		{"", "a"},
		{"files", ""},
	}
	for _, c := range cases {
		_, _, _, err := s.UploadFile(ctx, c.bucket, c.object, "text/plain", []byte("x"))
		require.True(t, storageV1.IsBadRequest(err), "upload %q/%q: %v", c.bucket, c.object, err)
		_, _, _, err = s.GetObjectReader(ctx, c.bucket, c.object)
		require.True(t, storageV1.IsBadRequest(err), "read %q/%q: %v", c.bucket, c.object, err)
		_, err = s.DownloadFile(ctx, downloadReq(c.bucket, c.object))
		require.Error(t, err, "download %q/%q", c.bucket, c.object)
		require.True(t, storageV1.IsBadRequest(s.DeleteFile(ctx, c.bucket, c.object)), "delete %q/%q", c.bucket, c.object)
	}

	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), got)
}

// 目录不能当对象读。
func TestLocalStorage_DirectoryIsNotAnObject(t *testing.T) {
	s, _ := newLocalForTest(t)
	ctx := context.Background()
	_, _, _, err := s.UploadFile(ctx, "files", "d/a.txt", "text/plain", []byte("x"))
	require.NoError(t, err)
	_, _, _, err = s.GetObjectReader(ctx, "files", "d")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// 根目录为空或不可写时构造失败，报错里带配置项名。
func TestNewLocalStorage_Errors(t *testing.T) {
	_, err := NewLocalStorage("", bLogger.NopLogger())
	require.ErrorContains(t, err, "oss.local.root_dir")

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ro := t.TempDir()
	require.NoError(t, os.Chmod(ro, 0o500))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	_, err = NewLocalStorage(ro, bLogger.NopLogger())
	require.ErrorContains(t, err, "not writable")

	// 根目录不存在时自动创建
	root := filepath.Join(t.TempDir(), "a", "b")
	_, err = NewLocalStorage(root, bLogger.NopLogger())
	require.NoError(t, err)
	st, err := os.Stat(root)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

// oss.storage 选择实现：留空和 minio 走 MinIO，local 走本地且不需要 MinIO 配置，其他值报错。
func TestNewStorage_Selects(t *testing.T) {
	minioCfg := &conf.OSS_MinIO{Endpoint: "127.0.0.1:1", AccessKey: "k", SecretKey: "s"}

	for _, kind := range []string{"", "minio", "MinIO"} {
		st, err := NewStorage(&conf.Bootstrap{Oss: &conf.OSS{Storage: kind, Minio: minioCfg}}, bLogger.NopLogger())
		require.NoError(t, err, kind)
		require.Equal(t, storageV1.OSSProvider_MINIO, st.Provider(), kind)
	}

	// 没有 minio 配置也能选 local（NewMinIoClient 缺配置会 log.Fatal，能走到这里说明没创建它）
	st, err := NewStorage(&conf.Bootstrap{Oss: &conf.OSS{
		Storage: "local", Local: &conf.OSS_Local{RootDir: t.TempDir()},
	}}, bLogger.NopLogger())
	require.NoError(t, err)
	require.Equal(t, storageV1.OSSProvider_LOCAL, st.Provider())

	_, err = NewStorage(&conf.Bootstrap{Oss: &conf.OSS{Storage: "local"}}, bLogger.NopLogger())
	require.ErrorContains(t, err, "root_dir")

	_, err = NewStorage(&conf.Bootstrap{Oss: &conf.OSS{Storage: "s3"}}, bLogger.NopLogger())
	require.ErrorContains(t, err, "unknown storage")
}

// 没写 provider 的旧记录按 MinIO 算；和当前存储不一致返回 STORAGE_PROVIDER_MISMATCH。
func TestCheckFileProvider(t *testing.T) {
	s, _ := newLocalForTest(t)
	require.NoError(t, CheckFileProvider(&storageV1.File{Provider: trans.Ptr(storageV1.OSSProvider_LOCAL)}, s))

	for _, f := range []*storageV1.File{
		{Id: trans.Ptr(uint32(1))},
		{Id: trans.Ptr(uint32(2)), Provider: trans.Ptr(storageV1.OSSProvider_MINIO)},
	} {
		err := CheckFileProvider(f, s)
		require.True(t, storageV1.IsStorageProviderMismatch(err), "%v", err)
	}
}
