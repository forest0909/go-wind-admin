package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"

	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/oss"
)

// 下载响应一律 attachment + nosniff，客户端传 disposition=inline 也不生效：
// 上传者控制的内容（这里是以 .html 命名的文本）不能在本站同源下被浏览器内联渲染。
func TestDownloadFile_AlwaysAttachmentAndNosniff(t *testing.T) {
	st, err := oss.NewLocalStorage(t.TempDir(), bLogger.NopLogger())
	require.NoError(t, err)
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	repo := data.NewFileRepo(boot, enttest.NewEntClientForTest(t))
	svc := service.NewFileTransferService(boot, st, repo)

	sysCtx := enttest.NewSystemContext(context.Background())
	body := []byte("hello<script>window.x=1</script>")
	_, _, _, err = st.UploadFile(sysCtx, "files", "temp/payload.html", "text/plain", body)
	require.NoError(t, err)
	require.NoError(t, repo.Create(sysCtx, &storageV1.CreateFileRequest{Data: &storageV1.File{
		Provider:      trans.Ptr(storageV1.OSSProvider_LOCAL),
		BucketName:    trans.Ptr("files"),
		FileDirectory: trans.Ptr("temp"),
		SaveFileName:  trans.Ptr("payload.html"),
		FileName:      trans.Ptr("payload.html"),
	}}))
	list, err := repo.List(sysCtx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetItems(), 1)
	id := list.GetItems()[0].GetId()

	srv := kratosHttp.NewServer()
	registerFileTransferServiceHandler(srv, svc)

	for _, q := range []string{"", "&disposition=inline"} {
		req := httptest.NewRequest(http.MethodGet, "/admin/v1/file/download?fileId="+strconv.FormatUint(uint64(id), 10)+q, nil).
			WithContext(sysCtx)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, body, rec.Body.Bytes())
		require.Contains(t, rec.Header().Get("Content-Type"), "text/plain", q)
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), q)
		require.Equal(t, `attachment; filename="temp/payload.html"`, rec.Header().Get("Content-Disposition"), q)
	}
}

func TestDownloadDisposition_SanitizesFilename(t *testing.T) {
	require.Equal(t, `attachment; filename="a_b_c_.txt"`, downloadDisposition("a\"b\\c\n.txt"))
}
