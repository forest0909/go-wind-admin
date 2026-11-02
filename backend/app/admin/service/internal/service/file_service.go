package service

import (
	"context"
	"strings"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/oss"
)

type FileService struct {
	adminV1.FileServiceHTTPServer

	log *bLogger.Helper

	fileRepo *data.FileRepo
	mc       *oss.MinIOClient
}

func NewFileService(
	ctx *bootstrap.Context,
	fileRepo *data.FileRepo,
	mc *oss.MinIOClient,
) *FileService {
	return &FileService{
		log:      ctx.NewLoggerHelper("file/service/admin-service"),
		fileRepo: fileRepo,
		mc:       mc,
	}
}

func (s *FileService) List(ctx context.Context, req *paginationV1.PagingRequest) (*storageV1.ListFileResponse, error) {
	return s.fileRepo.List(ctx, req)
}

func (s *FileService) Get(ctx context.Context, req *storageV1.GetFileRequest) (*storageV1.File, error) {
	return s.fileRepo.Get(ctx, req)
}

func (s *FileService) Create(ctx context.Context, req *storageV1.CreateFileRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)
	clearFileStorageFields(req.Data)

	if err = s.fileRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *FileService) Update(ctx context.Context, req *storageV1.UpdateFileRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())
	clearFileStorageFields(req.Data)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = dropFileStoragePaths(req.UpdateMask.Paths)
	}

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.fileRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *FileService) Delete(ctx context.Context, req *storageV1.DeleteFileRequest) (*emptypb.Empty, error) {
	f, err := s.fileRepo.Get(ctx, &storageV1.GetFileRequest{
		QueryBy: &storageV1.GetFileRequest_Id{Id: req.GetId()},
	})
	if err != nil {
		return nil, err
	}

	if err = s.fileRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	// 手工建的文件记录没有存储位置（见 clearFileStorageFields），没有对象可删。
	if f.GetBucketName() == "" || f.GetSaveFileName() == "" {
		return &emptypb.Empty{}, nil
	}

	if err = s.mc.DeleteFile(ctx,
		f.GetBucketName(),
		joinObjectPath(f.GetFileDirectory(), f.GetSaveFileName()),
	); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// fileStorageFields 文件记录里描述存储位置和大小的字段，只能由上传流程写入。
// 客户端能改的话，就能让记录指向别人的对象，再借 Delete 删掉它。
var fileStorageFields = map[string]struct{}{
	"bucketname": {}, "filedirectory": {}, "savefilename": {}, "size": {}, "sizeformat": {},
}

func clearFileStorageFields(f *storageV1.File) {
	f.BucketName = nil
	f.FileDirectory = nil
	f.SaveFileName = nil
	f.Size = nil
	f.SizeFormat = nil
}

// dropFileStoragePaths 去掉 update mask 里的存储字段，兼容 snake_case 和 camelCase。
func dropFileStoragePaths(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := fileStorageFields[strings.ToLower(strings.ReplaceAll(p, "_", ""))]; ok {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}
