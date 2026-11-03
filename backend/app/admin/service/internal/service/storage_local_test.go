package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/crypto"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/oss"
)

func serveAvatar(svc *UserProfileService, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	svc.ServeAvatar(rec, httptest.NewRequest(http.MethodGet, "http://x"+path, nil))
	return rec
}

// 上传头像后 user.avatar 存后端相对地址，免鉴权路由读得出 image/*；换头像删掉本人的旧对象。
func TestUploadAvatar_StoresBackendURLAndServes(t *testing.T) {
	e := newUserProfileServiceForTest(t)
	ctx := auth.NewContext(context.Background(), &authenticationV1.UserTokenPayload{UserId: 7})

	// 旧头像（新地址形式）已在存储里
	_, _, _, err := e.svc.storage.UploadFile(ctx, oss.AvatarBucket, "avatar/7/old.png", "image/png", tinyPNG)
	require.NoError(t, err)
	e.stub.getUserResult = &identityV1.User{Id: trans.Ptr(uint32(7)), Avatar: trans.Ptr(oss.AvatarURL(7, "old.png"))}

	resp, err := e.svc.UploadAvatar(ctx, &identityV1.UploadAvatarRequest{
		Source: &identityV1.UploadAvatarRequest_ImageBase64{ImageBase64: base64.StdEncoding.EncodeToString(tinyPNG)},
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(resp.GetUrl(), "/admin/v1/avatars/7/"), resp.GetUrl())
	require.Len(t, e.stub.capturedUpdates, 1)
	require.Equal(t, resp.GetUrl(), e.stub.capturedUpdates[0].GetData().GetAvatar())

	rec := serveAvatar(e.svc, resp.GetUrl())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, tinyPNG, rec.Body.Bytes())

	require.Equal(t, http.StatusNotFound, serveAvatar(e.svc, oss.AvatarURL(7, "old.png")).Code, "old avatar object should be deleted")
}

// 头像路由只读 images 桶 avatar/ 目录下的图片；其他路径、其他目录、非图片、不存在的对象一律 404。
func TestServeAvatar_RejectsNonAvatarObjects(t *testing.T) {
	e := newUserProfileServiceForTest(t)
	ctx := context.Background()
	st := e.svc.storage
	for _, o := range []struct{ bucket, object string }{
		{"images", "temp/x.png"},
		{"files", "avatar/7/x.png"},
		{"images", "avatar/x.png"},
	} {
		_, _, _, err := st.UploadFile(ctx, o.bucket, o.object, "image/png", tinyPNG)
		require.NoError(t, err)
	}
	_, _, _, err := st.UploadFile(ctx, "images", "avatar/7/note.txt", "text/plain", []byte("not an image"))
	require.NoError(t, err)
	// 扩展名是 .png、内容是 HTML/文本：按内容判断，不能当图片返回
	_, _, _, err = st.UploadFile(ctx, "images", "avatar/7/fake.png", "image/png", []byte("<html><script>alert(1)</script></html>"))
	require.NoError(t, err)
	_, _, _, err = st.UploadFile(ctx, "images", "avatar/7/x.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`))
	require.NoError(t, err)

	for _, p := range []string{
		"/admin/v1/avatars/7/../../temp/x.png",
		"/admin/v1/avatars/7/..%2F..%2Ftemp%2Fx.png",
		"/admin/v1/avatars/x.png",
		"/admin/v1/avatars/7/note.txt",
		"/admin/v1/avatars/7/fake.png",
		"/admin/v1/avatars/7/x.svg",
		"/admin/v1/avatars/7/missing.png",
		"/admin/v1/avatars/8/x.png",
		"/admin/v1/file/image",
	} {
		require.Equal(t, http.StatusNotFound, serveAvatar(e.svc, p).Code, p)
	}
}

// 换头像时认新旧两种地址；别人的、外链、非规范写法都不删。
func TestOwnedAvatarObject_NewURL(t *testing.T) {
	bucket, object, ok := ownedAvatarObject("", oss.AvatarURL(7, "a.png"), 7)
	require.True(t, ok)
	require.Equal(t, "images", bucket)
	require.Equal(t, "avatar/7/a.png", object)

	for _, u := range []string{
		oss.AvatarURL(8, "a.png"),
		"/admin/v1/avatars/7/../8/a.png",
		"https://example.com/admin/v1/avatars/7/a.png",
	} {
		_, _, ok := ownedAvatarObject("minio:9000", u, 7)
		require.False(t, ok, u)
	}
}

type localFileEnv struct {
	transfer *FileTransferService
	files    *FileService
	repo     *data.FileRepo
	root     string
	ctx      context.Context
}

func newLocalFileEnv(t *testing.T) localFileEnv {
	t.Helper()
	root := t.TempDir()
	st, err := oss.NewLocalStorage(root, bLogger.NopLogger())
	require.NoError(t, err)
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	repo := data.NewFileRepo(boot, enttest.NewEntClientForTest(t))
	return localFileEnv{
		transfer: &FileTransferService{log: bLogger.NewHelper(bLogger.NopLogger()), storage: st, fileServiceClient: repo},
		files:    &FileService{log: bLogger.NewHelper(bLogger.NopLogger()), storage: st, fileRepo: repo},
		repo:     repo,
		root:     root,
		ctx: auth.NewContext(enttest.NewSystemContext(context.Background()),
			&authenticationV1.UserTokenPayload{UserId: 7}),
	}
}

func (e localFileEnv) onlyFile(t *testing.T) *storageV1.File {
	t.Helper()
	resp, err := e.repo.List(e.ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetItems(), 1)
	return resp.GetItems()[0]
}

// 本地模式：上传记 provider=LOCAL，按 fileId 下载（含前端用的 preferPresignedUrl）经后端拿到内容，删除同时删掉文件。
func TestLocalStorage_FileUploadDownloadDelete(t *testing.T) {
	e := newLocalFileEnv(t)

	_, err := e.transfer.UploadFile(e.ctx, &storageV1.UploadFileRequest{
		Source:         &storageV1.UploadFileRequest_File{File: tinyPNG},
		Mime:           trans.Ptr("image/png"),
		SourceFileName: trans.Ptr("a.png"),
		StorageObject:  &storageV1.StorageObject{FileDirectory: trans.Ptr("temp")},
	})
	require.NoError(t, err)

	f := e.onlyFile(t)
	require.Equal(t, storageV1.OSSProvider_LOCAL, f.GetProvider())
	onDisk := filepath.Join(e.root, f.GetBucketName(), f.GetFileDirectory(), f.GetSaveFileName())
	_, err = os.Stat(onDisk)
	require.NoError(t, err)

	for _, presign := range []bool{false, true} {
		resp, derr := e.transfer.DownloadFile(e.ctx, &storageV1.DownloadFileRequest{
			Selector:           &storageV1.DownloadFileRequest_FileId{FileId: f.GetId()},
			PreferPresignedUrl: trans.Ptr(presign),
		})
		require.NoError(t, derr)
		require.Equal(t, tinyPNG, resp.GetFile())
	}

	_, err = e.files.Delete(e.ctx, &storageV1.DeleteFileRequest{QueryBy: &storageV1.DeleteFileRequest_Id{Id: f.GetId()}})
	require.NoError(t, err)
	_, err = os.Stat(onDisk)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// 已有记录存在 MinIO、当前配置是本地（中途换了存储）：下载、删除都返回 STORAGE_PROVIDER_MISMATCH，记录保留。
func TestLocalStorage_ProviderMismatch(t *testing.T) {
	e := newLocalFileEnv(t)
	require.NoError(t, e.repo.Create(e.ctx, &storageV1.CreateFileRequest{Data: &storageV1.File{
		Provider:     trans.Ptr(storageV1.OSSProvider_MINIO),
		BucketName:   trans.Ptr("images"),
		SaveFileName: trans.Ptr("x.png"),
		FileName:     trans.Ptr("x.png"),
	}}))
	f := e.onlyFile(t)
	require.Equal(t, storageV1.OSSProvider_MINIO, f.GetProvider())

	_, err := e.transfer.DownloadFile(e.ctx, &storageV1.DownloadFileRequest{
		Selector: &storageV1.DownloadFileRequest_FileId{FileId: f.GetId()},
	})
	require.True(t, storageV1.IsStorageProviderMismatch(err), "%v", err)

	_, err = e.files.Delete(e.ctx, &storageV1.DeleteFileRequest{QueryBy: &storageV1.DeleteFileRequest_Id{Id: f.GetId()}})
	require.True(t, storageV1.IsStorageProviderMismatch(err), "%v", err)
	require.Equal(t, f.GetId(), e.onlyFile(t).GetId(), "record must be kept")

	// 没有存储位置的手工记录不涉及存储，照常删除
	require.NoError(t, e.repo.Create(e.ctx, &storageV1.CreateFileRequest{Data: &storageV1.File{
		Provider: trans.Ptr(storageV1.OSSProvider_MINIO), FileName: trans.Ptr("manual"),
	}}))
	n, err := e.repo.List(e.ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, n.GetItems(), 2)
	var manualID uint32
	for _, it := range n.GetItems() {
		if it.GetId() != f.GetId() {
			manualID = it.GetId()
		}
	}
	_, err = e.files.Delete(e.ctx, &storageV1.DeleteFileRequest{QueryBy: &storageV1.DeleteFileRequest_Id{Id: manualID}})
	require.NoError(t, err)
}

// 头像只收 png/jpeg/gif/webp/bmp：SVG、HTML 等直接 400，不上传、不写库。
func TestUploadAvatar_RejectsNonBitmap(t *testing.T) {
	e := newUserProfileServiceForTest(t)
	e.stub.getUserResult = &identityV1.User{Id: trans.Ptr(uint32(7))}
	ctx := auth.NewContext(context.Background(), &authenticationV1.UserTokenPayload{UserId: 7})

	for name, body := range map[string][]byte{
		"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect/></svg>`),
		"html": []byte("<html><script>alert(1)</script></html>"),
	} {
		_, err := e.svc.UploadAvatar(ctx, &identityV1.UploadAvatarRequest{
			Source: &identityV1.UploadAvatarRequest_ImageBase64{ImageBase64: base64.StdEncoding.EncodeToString(body)},
		})
		require.Error(t, err, name)
		require.Contains(t, err.Error(), "only image files are allowed", name)
	}
	require.Empty(t, e.stub.capturedUpdates)
}

// 签名图片代理免鉴权：只返回位图并带 nosniff；文本文件即使以 .html 命名上传也拿不到（以前按扩展名返回 text/html，同源 XSS）。
func TestServeImageHandler_OnlySafeBitmaps(t *testing.T) {
	require.NoError(t, crypto.InitGlobalEncryptor("0123456789abcdef0123456789abcdef", true))
	e := newLocalFileEnv(t)

	upload := func(name string, body []byte, mimeType string) string {
		resp, err := e.transfer.UploadFile(e.ctx, &storageV1.UploadFileRequest{
			Source:         &storageV1.UploadFileRequest_File{File: body},
			Mime:           trans.Ptr(mimeType),
			SourceFileName: trans.Ptr(name),
		})
		require.NoError(t, err, name)
		require.NotEmpty(t, resp.GetPublicUrl(), name)
		return resp.GetPublicUrl()
	}
	get := func(u string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		require.NoError(t, e.transfer.ServeImageHandler(rec, httptest.NewRequest(http.MethodGet, "http://x"+u, nil)))
		return rec
	}

	rec := get(upload("a.png", tinyPNG, "image/png"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, tinyPNG, rec.Body.Bytes())

	rec = get(upload("payload.html", []byte("hello<script>window.reviewProbe=1</script>"), "text/html"))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotContains(t, rec.Body.String(), "<script>")
}

// 1x1 BMP（24 位，BITMAPINFOHEADER），按 BMP 规范手工拼出
var tinyBMP = []byte{
	'B', 'M', 58, 0, 0, 0, 0, 0, 0, 0, 54, 0, 0, 0, // 文件头：大小 58，像素偏移 54
	40, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 24, 0, // 信息头：40 字节，1x1，1 平面，24 位
	0, 0, 0, 0, 4, 0, 0, 0, 0x13, 0x0b, 0, 0, 0x13, 0x0b, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0x00, 0x00, 0xff, 0x00, // 一个红色像素 + 行填充
}

// BMP 一直是支持的图片类型：已有 BMP 头像、新上传的 BMP 头像都能读出来。
func TestAvatar_BMPSupported(t *testing.T) {
	e := newUserProfileServiceForTest(t)
	ctx := auth.NewContext(context.Background(), &authenticationV1.UserTokenPayload{UserId: 7})

	// 存储里已有的 BMP 头像（v3 改写后的地址形式）
	_, _, _, err := e.svc.storage.UploadFile(ctx, oss.AvatarBucket, "avatar/7/old.bmp", "image/bmp", tinyBMP)
	require.NoError(t, err)
	rec := serveAvatar(e.svc, oss.AvatarURL(7, "old.bmp"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/bmp", rec.Header().Get("Content-Type"))
	require.Equal(t, tinyBMP, rec.Body.Bytes())

	// 新上传 BMP 头像
	e.stub.getUserResult = &identityV1.User{Id: trans.Ptr(uint32(7))}
	resp, err := e.svc.UploadAvatar(ctx, &identityV1.UploadAvatarRequest{
		Source: &identityV1.UploadAvatarRequest_ImageBase64{ImageBase64: base64.StdEncoding.EncodeToString(tinyBMP)},
	})
	require.NoError(t, err)
	rec = serveAvatar(e.svc, resp.GetUrl())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/bmp", rec.Header().Get("Content-Type"))
	require.Equal(t, tinyBMP, rec.Body.Bytes())
}

// 普通上传（富文本插图走这条）的 BMP，上传返回的签名地址能读出来。
func TestServeImageHandler_BMP(t *testing.T) {
	require.NoError(t, crypto.InitGlobalEncryptor("0123456789abcdef0123456789abcdef", true))
	e := newLocalFileEnv(t)
	resp, err := e.transfer.UploadFile(e.ctx, &storageV1.UploadFileRequest{
		Source:         &storageV1.UploadFileRequest_File{File: tinyBMP},
		Mime:           trans.Ptr("image/bmp"),
		SourceFileName: trans.Ptr("a.bmp"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetPublicUrl())

	rec := httptest.NewRecorder()
	require.NoError(t, e.transfer.ServeImageHandler(rec, httptest.NewRequest(http.MethodGet, "http://x"+resp.GetPublicUrl(), nil)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/bmp", rec.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, tinyBMP, rec.Body.Bytes())
}
