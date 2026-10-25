package service

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"

	"github.com/tx7do/go-utils/ossutil"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
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
	mc                 *oss.MinIOClient

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
	mc *oss.MinIOClient,
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
		mc:                 mc,
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
	err = s.userCredentialRepo.ChangeCredential(ctx, &authenticationV1.ChangeCredentialRequest{
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
		// 校验图片大小（复用上传限制，头像不应超过该上限）
		if int64(len(imageBytes)) > ossutil.MaxUploadSize {
			return nil, authenticationV1.ErrorBadRequest("avatar exceeds max size")
		}
		// 嗅探真实图片类型并校验白名单
		realMime, _ := ossutil.DetectFileType(imageBytes)
		if !ossutil.IsAllowedMimeType(realMime) || len(realMime) < 6 || realMime[:6] != "image/" {
			return nil, authenticationV1.ErrorBadRequest("only image files are allowed for avatar")
		}
		// 上传到 OSS（mc 自动嗅探 MIME/桶/对象名，头像统一进 images 桶）
		_, _, downloadUrl, uerr := s.mc.UploadFile(ctx, "", "", realMime, imageBytes)
		if uerr != nil {
			s.log.Errorf(ctx, "upload avatar to oss failed [%s]", uerr.Error())
			return nil, authenticationV1.ErrorInternalServerError("upload avatar failed")
		}
		avatarURL = downloadUrl
	case *identityV1.UploadAvatarRequest_ImageUrl:
		avatarURL = req.GetImageUrl()
	default:
		s.log.Errorf(ctx, "upload avatar failed, invalid avatar source")
		return nil, authenticationV1.ErrorBadRequest("invalid avatar source")
	}

	if err = s.userRepo.Update(ctx, &identityV1.UpdateUserRequest{
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

	return &identityV1.UploadAvatarResponse{
		Url: avatarURL,
	}, nil
}
