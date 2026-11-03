package service

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"go-wind-admin/third_party/go-utils/ossutil"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/genproto/protobuf/field_mask"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/oss"
)

// profileEditableFields 个人资料页可以自行修改的字段（与 BaseSetting 表单一致）。
// 键为去掉下划线后的小写形式，兼容 snake_case 与 camelCase 两种 mask 写法。
var profileEditableFields = map[string]struct{}{
	"id": {}, "nickname": {}, "realname": {}, "email": {}, "mobile": {}, "telephone": {},
	"gender": {}, "region": {}, "address": {}, "description": {}, "locale": {}, "avatar": {},
}

// profileUpdatablePaths 只保留 profileEditableFields 内的路径；mask 为空时不做全量更新，
// 而是限定为全部可编辑字段，避免"无 mask = 全量"把其他字段一并写入。
func profileUpdatablePaths(paths []string) []string {
	if len(paths) == 0 {
		paths = make([]string, 0, len(profileEditableFields))
		for k := range profileEditableFields {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		return paths
	}
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := profileEditableFields[strings.ToLower(strings.ReplaceAll(p, "_", ""))]; ok {
			kept = append(kept, p)
		}
	}
	return kept
}

type UserProfileService struct {
	adminV1.UserProfileServiceHTTPServer

	userRepo           data.UserRepo
	roleRepo           *data.RoleRepo
	userCredentialRepo *data.UserCredentialRepo
	authenticator      *data.Authenticator
	outbound           *OutboundSender
	vcodeCache         *data.VCodeCache
	storage            oss.Storage
	// legacyAvatarHost 旧版头像地址的前缀（MinIO 的 download_host），只用来认出旧头像以便删除。
	legacyAvatarHost string

	log    *bLogger.Helper
	mailer *TransactionMailer
}

func NewUserProfileService(
	ctx *bootstrap.Context,
	userRepo data.UserRepo,
	roleRepo *data.RoleRepo,
	userCredentialRepo *data.UserCredentialRepo,
	authenticator *data.Authenticator,
	outbound *OutboundSender,
	vcodeCache *data.VCodeCache,
	storage oss.Storage,
	legacyAvatarHost string,
	mailer *TransactionMailer,
) *UserProfileService {
	return &UserProfileService{
		log:                ctx.NewLoggerHelper("user-profile/service/admin-service"),
		userRepo:           userRepo,
		roleRepo:           roleRepo,
		userCredentialRepo: userCredentialRepo,
		authenticator:      authenticator,
		outbound:           outbound,
		vcodeCache:         vcodeCache,
		storage:            storage,
		legacyAvatarHost:   legacyAvatarHost,
		mailer:             mailer,
	}
}

func (s *UserProfileService) GetUser(ctx context.Context, _ *emptypb.Empty) (*identityV1.User, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{
			Id: operator.UserId,
		},
	})
	if err != nil {
		s.log.Errorf(ctx, "查询用户失败[%s]", err.Error())
		return nil, authenticationV1.ErrorNotFound("user not found")
	}

	roleCodes, err := s.roleRepo.ListRoleCodesByRoleIds(ctx, user.GetRoleIds())
	if err != nil {
		s.log.Errorf(ctx, "get user role codes failed [%s]", err.Error())
	}
	if roleCodes != nil {
		user.Roles = roleCodes
	}

	return user, err
}

func (s *UserProfileService) UpdateUser(ctx context.Context, req *identityV1.UpdateUserRequest) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	req.Data.Id = trans.Ptr(operator.UserId)
	req.Id = operator.UserId
	// 个人资料只允许改资料字段。userRepo.Update 会按 mask 写角色、组织、岗位、状态等，
	// 不收窄的话任何登录用户都能把自己的 roleIds 改成管理员角色。
	req.UpdateMask = &field_mask.FieldMask{Paths: profileUpdatablePaths(req.GetUpdateMask().GetPaths())}

	if err = s.userRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *UserProfileService) ChangePassword(ctx context.Context, req *identityV1.ChangePasswordRequest) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// 前端与登录/注册一致走 AES 密文传输，必须解密后再校验/哈希——
	// 此前漏设 NeedDecrypt，密文串被当明文哈希入库（改完即锁号）且绕过复杂度校验。
	err = s.userCredentialRepo.ChangeCredential(ctx, operator.GetUserId(), &authenticationV1.ChangeCredentialRequest{
		IdentityType:  authenticationV1.UserCredential_USERNAME,
		Identifier:    operator.GetUsername(),
		OldCredential: req.GetOldPassword(),
		NewCredential: req.GetNewPassword(),
		NeedDecrypt:   true,
	})
	if err != nil {
		return nil, err
	}

	// 改密成功后吊销本人全部客户端类型的令牌（含当前会话），
	// 防止凭据泄露后旧令牌继续可用；前端在成功回调中引导重新登录。
	if err = s.authenticator.RevokeUserTokenAllClientTypes(ctx, operator.GetUserId()); err != nil {
		s.log.Errorf(ctx, "revoke tokens after password change failed for user [%d]: %v", operator.GetUserId(), err)
	}

	return &emptypb.Empty{}, nil
}

// DeleteAvatar 删除头像
func (s *UserProfileService) DeleteAvatar(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err = s.userRepo.Update(ctx, &identityV1.UpdateUserRequest{
		Id: operator.UserId,
		Data: &identityV1.User{
			Id:     trans.Ptr(operator.UserId),
			Avatar: trans.Ptr(""),
		},
		UpdateMask: &field_mask.FieldMask{
			Paths: []string{"avatar"},
		},
	}); err != nil {
		s.log.Errorf(ctx, "delete user avatar failed [%s]", err.Error())
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// UploadAvatar 上传头像
func (s *UserProfileService) UploadAvatar(ctx context.Context, req *identityV1.UploadAvatarRequest) (*identityV1.UploadAvatarResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// 先记下旧头像，新头像写库成功后删掉旧对象。
	var oldAvatar string
	if user, gerr := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{Id: operator.GetUserId()},
	}); gerr != nil {
		s.log.Errorf(ctx, "get user before avatar upload failed, old avatar object will be kept: %v", gerr)
	} else {
		oldAvatar = user.GetAvatar()
	}

	var avatarURL string
	switch req.GetSource().(type) {
	case *identityV1.UploadAvatarRequest_ImageBase64:
		// 解码 base64 图片数据
		imageBytes, derr := base64.StdEncoding.DecodeString(req.GetImageBase64())
		if derr != nil {
			s.log.Errorf(ctx, "decode avatar base64 failed [%s]", derr.Error())
			return nil, authenticationV1.ErrorBadRequest("invalid avatar base64 data")
		}
		if len(imageBytes) == 0 {
			return nil, authenticationV1.ErrorBadRequest("empty avatar data")
		}
		// 校验图片大小（与前端 BaseSetting 的 5 MiB 一致）
		if int64(len(imageBytes)) > MaxAvatarBytes {
			return nil, authenticationV1.ErrorBadRequest("avatar exceeds max size")
		}
		// 嗅探真实图片类型并校验白名单
		realMime, _ := ossutil.DetectFileType(imageBytes)
		// 头像经免鉴权路由同源返回，只收可安全内联的位图（png/jpeg/gif/webp/bmp），不收 SVG 等。
		if !oss.IsSafeInlineImage(realMime) {
			return nil, authenticationV1.ErrorBadRequest("only image files are allowed for avatar")
		}
		// 上传到 images 桶，对象名放在本人的头像目录下，换头像时只删得掉这个目录里的旧对象。
		name := ossutil.GenerateFileName(imageBytes, ossutil.ContentTypeToFileExtension(realMime), ossutil.GenerateFileNameTypeUUID)
		if _, _, _, uerr := s.storage.UploadFile(ctx,
			oss.AvatarBucket, oss.AvatarDirectory(operator.GetUserId())+"/"+name,
			realMime, imageBytes); uerr != nil {
			s.log.Errorf(ctx, "upload avatar to oss failed: %v", uerr)
			return nil, authenticationV1.ErrorInternalServerError("upload avatar failed")
		}
		// 存后端相对地址，由 ServeAvatar 从存储读出返回（见 pkg/oss/avatar.go）。
		avatarURL = oss.AvatarURL(operator.GetUserId(), name)
	case *identityV1.UploadAvatarRequest_ImageUrl:
		avatarURL = req.GetImageUrl()
	default:
		s.log.Errorf(ctx, "upload avatar failed, invalid avatar source")
		return nil, authenticationV1.ErrorBadRequest("invalid avatar source")
	}

	if err = s.userRepo.Update(ctx, &identityV1.UpdateUserRequest{
		Id: operator.UserId,
		Data: &identityV1.User{
			Id:     trans.Ptr(operator.UserId),
			Avatar: trans.Ptr(avatarURL),
		},
		UpdateMask: &field_mask.FieldMask{
			Paths: []string{"avatar"},
		},
	}); err != nil {
		s.log.Errorf(ctx, "delete user avatar failed [%s]", err.Error())
		return nil, err
	}

	if oldAvatar != "" && oldAvatar != avatarURL {
		if bucket, object, ok := ownedAvatarObject(s.legacyAvatarHost, oldAvatar, operator.GetUserId()); ok {
			if derr := s.storage.DeleteFile(ctx, bucket, object); derr != nil {
				// 新头像已生效，旧对象删不掉只留下孤儿对象，不让本次请求失败。
				s.log.Errorf(ctx, "delete old avatar object %s/%s failed: %v", bucket, object, derr)
			}
		}
	}

	return &identityV1.UploadAvatarResponse{
		Url: avatarURL,
	}, nil
}

// MaxAvatarBytes 头像解码后的大小上限，与前端上传前的 5 MiB 校验一致。
// REST 默认请求体上限按它推算（见 server/body_limit.go）。
const MaxAvatarBytes int64 = 5 << 20

// ownedAvatarObject 判断头像地址是否指向本服务给该用户上传的头像对象，是则返回桶和对象名。
// 认两种地址：现在的 /admin/v1/avatars/{userId}/{name}，和旧版的 {download_host}/images/avatar/{userId}/{name}。
// 头像也可能是任意外链（ImageUrl），或者指向别人的对象，这些都不能删。
func ownedAvatarObject(legacyHost, avatarURL string, userID uint32) (bucket, object string, ok bool) {
	if uid, obj, isNew := oss.ParseAvatarURL(avatarURL); isNew {
		if uid != userID {
			return "", "", false
		}
		return oss.AvatarBucket, obj, true
	}
	if obj, isOld := oss.LegacyAvatarObject(legacyHost, avatarURL, userID); isOld {
		return oss.AvatarBucket, obj, true
	}
	return "", "", false
}

// ServeAvatar 头像读取（免鉴权，<img> 带不了 Bearer token）：GET /admin/v1/avatars/{userId}/{name}。
// 只读 images 桶 avatar/ 目录下的位图（png/jpeg/gif/webp/bmp），对象名是 UUID，猜不到。其他一律 404。
func (s *UserProfileService) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	_, object, ok := oss.ParseAvatarURL(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	reader, contentType, size, err := s.storage.GetObjectReader(r.Context(), oss.AvatarBucket, object)
	if err != nil {
		// 实现内已记录原始错误
		http.NotFound(w, r)
		return
	}
	defer func() {
		if cerr := reader.Close(); cerr != nil {
			s.log.Warnf(r.Context(), "close avatar reader %s failed: %v", object, cerr)
		}
	}()
	if !oss.IsSafeInlineImage(contentType) {
		s.log.Warnf(r.Context(), "avatar object %s has content type %q, not a safe inline image, refused", object, contentType)
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 对象名唯一（换头像会生成新名字），可以长期缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err = io.Copy(w, reader); err != nil {
		s.log.Warnf(r.Context(), "write avatar %s failed: %v", object, err)
	}
}
