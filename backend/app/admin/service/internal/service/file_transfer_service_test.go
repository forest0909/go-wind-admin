package service

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/oss"
)

// fakeS3 只实现上传路径会用到的几个 S3 接口，记录收到的 PUT 对象路径。
type fakeS3 struct {
	mu   sync.Mutex
	puts []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Has("location"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
	case r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		f.mu.Lock()
		f.puts = append(f.puts, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func newFileTransferServiceForTest(t *testing.T) (*FileTransferService, *fakeS3) {
	t.Helper()
	s3 := &fakeS3{}
	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	mc := oss.NewMinIoClient(&conf.Bootstrap{Oss: &conf.OSS{Minio: &conf.OSS_MinIO{
		Endpoint: host, UploadHost: host, DownloadHost: host, AccessKey: "k", SecretKey: "s",
	}}}, bLogger.NopLogger())

	return &FileTransferService{
		log:               bLogger.NewHelper(bLogger.NopLogger()),
		mc:                mc,
		fileServiceClient: data.NewFileRepo(boot, enttest.NewEntClientForTest(t)),
	}, s3
}

// 1x1 PNG
var tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

// 客户端指定的 bucket/objectName 必须被忽略，否则可以覆盖别人的对象。
func TestFileTransferService_UploadIgnoresClientObjectName(t *testing.T) {
	svc, s3 := newFileTransferServiceForTest(t)
	ctx := auth.NewContext(enttest.NewSystemContext(context.Background()),
		&authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.UploadFile(ctx, &storageV1.UploadFileRequest{
		Source:         &storageV1.UploadFileRequest_File{File: tinyPNG},
		Mime:           trans.Ptr("image/png"),
		SourceFileName: trans.Ptr("a.png"),
		StorageObject: &storageV1.StorageObject{
			BucketName:    trans.Ptr("victim-bucket"),
			ObjectName:    trans.Ptr("victim/secret.png"),
			FileDirectory: trans.Ptr("temp"),
		},
	})
	require.NoError(t, err)
	require.Len(t, s3.puts, 1)
	require.True(t, strings.HasPrefix(s3.puts[0], "/images/temp/"), "object should be server-generated, got %s", s3.puts[0])
	require.NotContains(t, s3.puts[0], "victim")
}

// 不传 storageObject 也能上传（编辑器图片上传不需要指定目录）。
func TestFileTransferService_UploadWithoutStorageObject(t *testing.T) {
	svc, s3 := newFileTransferServiceForTest(t)
	ctx := auth.NewContext(enttest.NewSystemContext(context.Background()),
		&authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.UploadFile(ctx, &storageV1.UploadFileRequest{
		Source:         &storageV1.UploadFileRequest_File{File: tinyPNG},
		Mime:           trans.Ptr("image/png"),
		SourceFileName: trans.Ptr("a.png"),
	})
	require.NoError(t, err)
	require.Len(t, s3.puts, 1)
	require.True(t, strings.HasPrefix(s3.puts[0], "/images/"))
}

// 按 bucket/objectName 直接下载会绕过文件表归属检查，必须拒绝。
func TestFileTransferService_DownloadByStorageObjectRejected(t *testing.T) {
	svc, _ := newFileTransferServiceForTest(t)
	_, err := svc.DownloadFile(context.Background(), &storageV1.DownloadFileRequest{
		Selector: &storageV1.DownloadFileRequest_StorageObject{StorageObject: &storageV1.StorageObject{
			BucketName: trans.Ptr("images"), ObjectName: trans.Ptr("someone/else.png"),
		}},
	})
	require.Error(t, err)
	require.True(t, storageV1.IsBadRequest(err), "got %v", err)
}
