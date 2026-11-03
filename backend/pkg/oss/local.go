package oss

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go-wind-admin/third_party/go-utils/ossutil"
	"go-wind-admin/third_party/go-utils/timeutil"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

// 本地存储的目录和文件权限：属主读写，同组只读，其他用户无权限。
const (
	localDirPerm  fs.FileMode = 0o750
	localFilePerm fs.FileMode = 0o640
)

// errSymlink 对象路径上出现符号链接。
var errSymlink = errors.New("symlink in object path")

// LocalStorage 本地磁盘存储，对象存放在 root/bucket/object。
// 所有读写删都经 os.Root 进行，不会落到根目录之外；另外拒绝路径上的任何符号链接。
type LocalStorage struct {
	dir  string   // 解析过符号链接的根目录，只用于日志和报错
	root *os.Root // 进程生命周期内不关闭
	log  *bLogger.Helper
}

var _ Storage = (*LocalStorage)(nil)

// NewLocalStorage 创建本地存储。根目录不存在就创建；不可写时返回错误。
func NewLocalStorage(rootDir string, logger bLogger.Logger) (*LocalStorage, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, errors.New("oss.local.root_dir is required when oss.storage is local")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: %w", rootDir, err)
	}
	if err = os.MkdirAll(abs, localDirPerm); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: create directory: %w", abs, err)
	}
	// 根目录本身可以是符号链接（如 macOS 的 /tmp），按解析后的真实目录打开。
	dir, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: resolve: %w", abs, err)
	}
	probe, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q is not writable: %w", dir, err)
	}
	name := probe.Name()
	if err = probe.Close(); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: close probe file: %w", dir, err)
	}
	if err = os.Remove(name); err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: remove probe file: %w", dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("oss.local.root_dir %q: open: %w", dir, err)
	}
	return &LocalStorage{
		dir:  dir,
		root: root,
		log:  bLogger.NewHelper(logger.With("module", "oss/local")),
	}, nil
}

func (s *LocalStorage) Provider() storageV1.OSSProvider { return storageV1.OSSProvider_LOCAL }

// objectPath 把 bucket/object 映射成相对根目录的路径（斜杠分隔，os.Root 接受）。
// 拒绝空段、"."、".."、绝对路径、反斜杠和 NUL。
func objectPath(bucketName, objectName string) (string, error) {
	if bucketName == "" || objectName == "" {
		return "", storageV1.ErrorBadRequest("bucket name and object name are required")
	}
	if strings.ContainsAny(bucketName, `/\`) || strings.ContainsRune(bucketName, 0) ||
		bucketName == "." || bucketName == ".." {
		return "", storageV1.ErrorBadRequest("invalid bucket name")
	}
	if strings.HasPrefix(objectName, "/") || strings.Contains(objectName, `\`) ||
		strings.ContainsRune(objectName, 0) || filepath.IsAbs(objectName) {
		return "", storageV1.ErrorBadRequest("invalid object name")
	}
	for _, seg := range strings.Split(objectName, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", storageV1.ErrorBadRequest("invalid object name")
		}
	}
	return bucketName + "/" + objectName, nil
}

// checkNoSymlink 逐级 Lstat，路径上（中间目录和最终文件）出现符号链接就拒绝。
// 遇到不存在的一级就停：后面的都还不存在，会由本服务按普通目录/文件创建。
// os.Root 本身已保证不会越出根目录；这里再拒绝根目录内的链接，避免链接把两个对象指到一起。
func (s *LocalStorage) checkNoSymlink(rel string) error {
	segs := strings.Split(rel, "/")
	for i := range segs {
		p := strings.Join(segs[:i+1], "/")
		st, err := s.root.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", p, errSymlink)
		}
	}
	return nil
}

// resolve 校验对象名并检查符号链接，返回相对路径。
func (s *LocalStorage) resolve(bucketName, objectName string) (string, error) {
	rel, err := objectPath(bucketName, objectName)
	if err != nil {
		return "", err
	}
	if err = s.checkNoSymlink(rel); err != nil {
		return "", err
	}
	return rel, nil
}

// UploadFile 先写同目录下的临时文件，再 rename 到目标位置，读的人看不到写了一半的文件。
func (s *LocalStorage) UploadFile(ctx context.Context, bucketName, objectName, mimeType string, fileContent []byte) (UploadInfo, string, string, error) {
	if len(fileContent) == 0 {
		return UploadInfo{}, "", "", storageV1.ErrorUploadFailed("empty fileContent data")
	}
	rel, err := objectPath(bucketName, objectName)
	if err != nil {
		return UploadInfo{}, "", "", err
	}
	if err = s.writeFile(rel, fileContent); err != nil {
		s.log.Errorf(ctx, "local storage: write %s/%s under %s failed: %v", bucketName, objectName, s.dir, err)
		if errors.Is(err, errSymlink) {
			return UploadInfo{}, "", "", storageV1.ErrorBadRequest("invalid object path")
		}
		return UploadInfo{}, "", "", storageV1.ErrorUploadFailed("failed to upload fileContent")
	}
	info := UploadInfo{Bucket: bucketName, Key: objectName, Size: int64(len(fileContent))}
	return info, ossutil.JoinObjectUrl("", bucketName, objectName), "", nil
}

func randomSuffix() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *LocalStorage) writeFile(rel string, content []byte) (err error) {
	if err = s.checkNoSymlink(rel); err != nil {
		return err
	}
	dir := path.Dir(rel)
	if err = s.root.MkdirAll(dir, localDirPerm); err != nil {
		return err
	}
	// MkdirAll 之后再查一次：新建的目录不会是链接，并发出现的链接在这里拦下（越界由 os.Root 兜底）。
	if err = s.checkNoSymlink(rel); err != nil {
		return err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tmpName := dir + "/.upload-" + suffix
	tmp, err := s.root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, localFilePerm)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rmErr := s.root.Remove(tmpName); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				err = errors.Join(err, rmErr)
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		_ = tmp.Close() // 写入已失败，返回写入错误
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close() // 同上
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// OpenFile 的权限受 umask 影响，显式设一次
	if err = s.root.Chmod(tmpName, localFilePerm); err != nil {
		return err
	}
	// 目标若是链接，rename 只替换链接本身，不会写到链接指向的位置
	return s.root.Rename(tmpName, rel)
}

func (s *LocalStorage) DeleteFile(ctx context.Context, bucketName, objectName string) error {
	rel, err := s.resolve(bucketName, objectName)
	if errors.Is(err, errSymlink) {
		s.log.Errorf(ctx, "local storage: refuse to delete %s/%s: %v", bucketName, objectName, err)
		return storageV1.ErrorBadRequest("invalid object path")
	}
	if err != nil {
		return err
	}
	if err = s.root.Remove(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.log.Errorf(ctx, "local storage: delete %s/%s failed: %v", bucketName, objectName, err)
		return storageV1.ErrorDeleteFailed("failed to delete file")
	}
	return nil
}

// open 打开对象文件并确认是普通文件。
func (s *LocalStorage) open(bucketName, objectName string) (*os.File, fs.FileInfo, error) {
	rel, err := s.resolve(bucketName, objectName)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.root.Open(rel)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close() // 返回 Stat 的错误
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		_ = f.Close() // 不是普通文件，按不存在处理
		return nil, nil, fmt.Errorf("%s/%s: %w", bucketName, objectName, fs.ErrNotExist)
	}
	return f, st, nil
}

// contentTypeOf 本地文件不存 Content-Type，按内容嗅探，和上传时校验用的是同一个函数
// （ossutil.DetectFileType 只看前 512 字节），所以得到的就是上传时校验过的类型。
// 不看扩展名：文件名由上传者决定，不能让它把类型提升成 HTML 之类的主动内容。
func contentTypeOf(f io.ReadSeeker) (string, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if n == 0 {
		return DefaultContentType, nil
	}
	ct, _ := ossutil.DetectFileType(head[:n])
	if ct == "" {
		ct = DefaultContentType
	}
	return ct, nil
}

func (s *LocalStorage) GetObjectReader(ctx context.Context, bucketName, objectName string) (io.ReadCloser, string, int64, error) {
	f, st, err := s.open(bucketName, objectName)
	if err != nil {
		s.log.Errorf(ctx, "local storage: open %s/%s failed: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	ct, err := contentTypeOf(f)
	if err != nil {
		_ = f.Close() // 返回检测类型时的错误
		s.log.Errorf(ctx, "local storage: detect content type of %s/%s failed: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	return f, ct, st.Size(), nil
}

// DownloadFile 本地存储没有预签名地址，PreferPresignedUrl 也直接返回文件内容。
// Range 语义和 MinIO 一致：闭区间 [start, end]，只给 end 时表示从 0 到 end。
func (s *LocalStorage) DownloadFile(ctx context.Context, req *storageV1.DownloadFileRequest) (*storageV1.DownloadFileResponse, error) {
	if _, ok := req.Selector.(*storageV1.DownloadFileRequest_StorageObject); !ok {
		return nil, storageV1.ErrorBadRequest("invalid selector")
	}
	bucketName := req.GetStorageObject().GetBucketName()
	objectName := req.GetStorageObject().GetObjectName()

	f, st, err := s.open(bucketName, objectName)
	if err != nil {
		s.log.Errorf(ctx, "local storage: open %s/%s failed: %v", bucketName, objectName, err)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, storageV1.ErrorFileNotFound("file not found")
		case errors.Is(err, errSymlink):
			return nil, storageV1.ErrorBadRequest("invalid object path")
		case storageV1.IsBadRequest(err):
			return nil, err
		}
		return nil, storageV1.ErrorDownloadFailed("failed to get object")
	}
	defer func() { _ = f.Close() }() // 只读文件，关闭错误无影响

	start, end := int64(0), st.Size()-1
	if req.RangeStart != nil {
		start = req.GetRangeStart()
	}
	if req.RangeEnd != nil && req.GetRangeEnd() < end {
		end = req.GetRangeEnd()
	}
	if start < 0 || (st.Size() > 0 && start > end) {
		return nil, storageV1.ErrorBadRequest("invalid range")
	}

	buf := new(bytes.Buffer)
	if _, err = buf.ReadFrom(io.NewSectionReader(f, start, end-start+1)); err != nil {
		s.log.Errorf(ctx, "local storage: read %s/%s failed: %v", bucketName, objectName, err)
		return nil, storageV1.ErrorDownloadFailed("failed to read object")
	}

	resp := &storageV1.DownloadFileResponse{
		Content: &storageV1.DownloadFileResponse_File{File: buf.Bytes()},
	}
	if req.GetAcceptMime() != "" {
		resp.Mime = req.GetAcceptMime()
	} else if resp.Mime, err = contentTypeOf(f); err != nil {
		s.log.Errorf(ctx, "local storage: detect content type of %s/%s failed: %v", bucketName, objectName, err)
		return nil, storageV1.ErrorDownloadFailed("failed to read object")
	}
	modTime := st.ModTime()
	resp.SourceFileName = objectName
	resp.Size = st.Size()
	resp.UpdatedAt = timeutil.TimeToTimestamppb(&modTime)
	return resp, nil
}
