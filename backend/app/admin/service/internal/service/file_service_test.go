package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
)

func newFileServiceForTest(t *testing.T) (*FileService, context.Context) {
	t.Helper()
	boot := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	svc := &FileService{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		fileRepo: data.NewFileRepo(boot, enttest.NewEntClientForTest(t)),
	}
	ctx := auth.NewContext(enttest.NewSystemContext(context.Background()),
		&authenticationV1.UserTokenPayload{UserId: 7})
	return svc, ctx
}

// 客户端不能通过 FileService.Update 改存储位置和大小，其他字段照常更新。
func TestFileService_UpdateIgnoresStorageFields(t *testing.T) {
	svc, ctx := newFileServiceForTest(t)
	require.NoError(t, svc.fileRepo.Create(ctx, &storageV1.CreateFileRequest{Data: &storageV1.File{
		Id:            trans.Ptr(uint32(1)),
		BucketName:    trans.Ptr("images"),
		FileDirectory: trans.Ptr("temp"),
		SaveFileName:  trans.Ptr("a.png"),
		FileName:      trans.Ptr("a.png"),
		Size:          trans.Ptr(uint64(100)),
	}}))

	_, err := svc.Update(ctx, &storageV1.UpdateFileRequest{
		Id: 1,
		Data: &storageV1.File{
			BucketName:    trans.Ptr("victim"),
			FileDirectory: trans.Ptr("other"),
			SaveFileName:  trans.Ptr("secret.png"),
			Size:          trans.Ptr(uint64(1)),
			FileName:      trans.Ptr("renamed.png"),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"bucket_name", "fileDirectory", "save_file_name", "size", "file_name"}},
	})
	require.NoError(t, err)

	got, err := svc.fileRepo.Get(ctx, &storageV1.GetFileRequest{QueryBy: &storageV1.GetFileRequest_Id{Id: 1}})
	require.NoError(t, err)
	require.Equal(t, "images", got.GetBucketName())
	require.Equal(t, "temp", got.GetFileDirectory())
	require.Equal(t, "a.png", got.GetSaveFileName())
	require.Equal(t, uint64(100), got.GetSize())
	require.Equal(t, "renamed.png", got.GetFileName())
}

// FileService.Create 丢弃客户端给的存储字段；这种记录删除时不去碰对象存储。
func TestFileService_CreateIgnoresStorageFields(t *testing.T) {
	svc, ctx := newFileServiceForTest(t)
	_, err := svc.Create(ctx, &storageV1.CreateFileRequest{Data: &storageV1.File{
		Id:            trans.Ptr(uint32(2)),
		BucketName:    trans.Ptr("victim"),
		FileDirectory: trans.Ptr("other"),
		SaveFileName:  trans.Ptr("secret.png"),
		Size:          trans.Ptr(uint64(1)),
		FileName:      trans.Ptr("b.png"),
	}})
	require.NoError(t, err)

	got, err := svc.fileRepo.Get(ctx, &storageV1.GetFileRequest{QueryBy: &storageV1.GetFileRequest_Id{Id: 2}})
	require.NoError(t, err)
	require.Empty(t, got.GetBucketName())
	require.Empty(t, got.GetFileDirectory())
	require.Empty(t, got.GetSaveFileName())
	require.Zero(t, got.GetSize())

	// mc 为 nil：若仍按存储字段删对象会 panic。
	_, err = svc.Delete(ctx, &storageV1.DeleteFileRequest{QueryBy: &storageV1.DeleteFileRequest_Id{Id: 2}})
	require.NoError(t, err)
}
