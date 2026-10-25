package server

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/middleware/validate"
	"github.com/go-kratos/kratos/v2/transport/http"

	authz "github.com/tx7do/kratos-authz/middleware"

	swaggerUI "github.com/tx7do/kratos-swagger-ui"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/rpc"

	"go-wind-admin/app/admin/service/cmd/server/assets"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	auditV1 "go-wind-admin/api/gen/go/audit/service/v1"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/authorizer"
	"go-wind-admin/pkg/middleware/auth"
	applogging "go-wind-admin/pkg/middleware/logging"
)

// NewRestMiddleware 创建中间件
func NewRestMiddleware(
	ctx *bootstrap.Context,
	accessTokenChecker auth.AccessTokenChecker,
	tenantAccessChecker auth.TenantAccessChecker,
	authorizer *authorizer.Authorizer,
	loginLogRepo *data.LoginAuditLogRepo,
	operationAuditLogRepo *data.OperationAuditLogRepo,
) []middleware.Middleware {
	var ms []middleware.Middleware
	// recovery 必须置于链首：任何中间件/handler 的 panic（如审计日志解析畸形 JWT）
	// 兜底为 500，避免崩溃请求 goroutine、被用作未认证 DoS。
	ms = append(ms, recovery.Recovery())
	ms = append(ms, logging.Server(bLogger.AsKratosLogger(ctx.GetLogger())))

	ms = append(ms, applogging.Server(
		applogging.WithWriteLoginLogFunc(func(ctx context.Context, data *auditV1.LoginAuditLog) error {
			// TODO 如果系统的负载比较小，可以同步写入数据库，否则，建议使用异步方式，即投递进队列。
			return loginLogRepo.Create(ctx, &auditV1.CreateLoginAuditLogRequest{Data: data})
		}),
		applogging.WithWriteOperationAuditLogFunc(func(ctx context.Context, data *auditV1.OperationAuditLog) error {
			return operationAuditLogRepo.Create(ctx, &auditV1.CreateOperationAuditLogRequest{Data: data})
		}),
	))

	// 输入校验：对所有 RPC（含白名单内的 login/refresh）调用生成代码的
	// Validate()。放 selector 外，否则白名单路由会被跳过——而它们恰是最需要校验入参的。
	// 当前业务 proto 尚未补 (validate.rules)，多数 Validate() 返回 nil；补规则后再生效。
	ms = append(ms, validate.Validator())

	// add white list for authentication.
	rpc.AddWhiteList(
		adminV1.OperationAuthenticationServiceLogin,
		adminV1.OperationAuthenticationServiceGenerateCaptcha,
		adminV1.OperationAuthenticationServiceVerifyCaptcha,
		// 刷新令牌接口免鉴权：refresh token 现以 HttpOnly Cookie 传输且为自描述 JWT，
		// 可脱离 access token 独立鉴权。页面刷新后 access token 丢失时，前端凭
		// refresh cookie 静默恢复会话，不再强制重新登录。
		adminV1.OperationAuthenticationServiceRefreshToken,
		// 租户白标查询免鉴权：登录前按租户编号取名称/Logo（只暴露展示字段）
		adminV1.OperationAuthenticationServiceGetTenantBranding,
		//OperationFileTransferServiceDownloadFile,
		//OperationFileTransferServicePostUploadFile,
		//OperationFileTransferServicePutUploadFile,
	)

	ms = append(ms, selector.Server(
		auth.Server(
			auth.WithAccessTokenChecker(accessTokenChecker),
			auth.WithTenantAccessChecker(tenantAccessChecker),
			auth.WithInjectMetadata(false),
			auth.WithInjectEnt(true),
		),
		authz.Server(authorizer.Engine()),
	).
		Match(rpc.NewRestWhiteListMatcher()).
		Build(),
	)

	return ms
}

// NewRestServer new an REST server.
func NewRestServer(
	ctx *bootstrap.Context,

	middlewares []middleware.Middleware,
	authorizer *authorizer.Authorizer,

	authenticationService *service.AuthenticationService,

	portalService *service.AdminPortalService,
	taskService *service.TaskService,

	fileService *service.FileService,
	fileTransferService *service.FileTransferService,

	dictTypeService *service.DictTypeService,
	dictEntryService *service.DictEntryService,
	languageService *service.LanguageService,

	tenantService *service.TenantService,
	planService *service.PlanService,
	planQuotaService *service.PlanQuotaService,
	planModuleService *service.PlanModuleService,
	userService *service.UserService,
	userProfileService *service.UserProfileService,
	roleService *service.RoleService,
	positionService *service.PositionService,
	orgUnitService *service.OrgUnitService,

	menuService *service.MenuService,
	apiService *service.ApiService,
	permissionService *service.PermissionService,
	permissionGroupService *service.PermissionGroupService,

	loginAuditLogService *service.LoginAuditLogService,
	operationAuditLogService *service.OperationAuditLogService,
	redisCacheMonitorService *service.RedisCacheMonitorService,
	serverMonitorService *service.ServerMonitorService,
	onlineSessionService *service.OnlineSessionService,
	dashboardService *service.DashboardService,

	internalMessageService *service.InternalMessageService,
	internalMessageCategoryService *service.InternalMessageCategoryService,
	internalMessageRecipientService *service.InternalMessageRecipientService,

	// register:param ── 新模块服务形参在此行后注册(make register 工具锚点,勿删)
	myTenantUsageService *service.MyTenantUsageService,
	auditExportService *service.AuditExportService,
	taskMonitorService *service.TaskMonitorService,
	configService *service.ConfigService,
) (*http.Server, error) {
	cfg := ctx.GetConfig()

	if cfg == nil || cfg.Server == nil || cfg.Server.Rest == nil {
		return nil, nil
	}

	srv, err := rpc.CreateRestServer(cfg,
		middlewares...,
	)
	if err != nil {
		return nil, err
	}

	apiService.RegisterRouteWalker(srv)

	adminV1.RegisterAuthenticationServiceHTTPServer(srv, authenticationService)

	adminV1.RegisterUserProfileServiceHTTPServer(srv, userProfileService)

	adminV1.RegisterAdminPortalServiceHTTPServer(srv, portalService)
	adminV1.RegisterTaskServiceHTTPServer(srv, taskService)

	adminV1.RegisterDictTypeServiceHTTPServer(srv, dictTypeService)
	adminV1.RegisterDictEntryServiceHTTPServer(srv, dictEntryService)
	adminV1.RegisterLanguageServiceHTTPServer(srv, languageService)

	adminV1.RegisterApiServiceHTTPServer(srv, apiService)
	adminV1.RegisterMenuServiceHTTPServer(srv, menuService)
	adminV1.RegisterPermissionServiceHTTPServer(srv, permissionService)
	adminV1.RegisterPermissionGroupServiceHTTPServer(srv, permissionGroupService)

	// 字段权限装饰器在最内层（紧贴业务实现），静态脱敏 redact 在外层兜底；
	// 两者都按"清值"裁剪，protojson 默认不输出未填充字段，效果等价于移除字段。
	adminV1.RegisterUserServiceHTTPServer(srv, adminV1.RedactedUserServiceServer(
		service.NewFieldPermissionUserServiceServer(&userServiceServerAdapter{UserServiceHTTPServer: userService}),
		nil))
	// Role 资源同款装饰器（字段权限铺开第二个资源）：可管控字段 permissions
	// （角色权限集，权限体系的元权限）。
	adminV1.RegisterRoleServiceHTTPServer(srv, service.NewFieldPermissionRoleServiceServer(roleService))
	adminV1.RegisterOrgUnitServiceHTTPServer(srv, orgUnitService)
	adminV1.RegisterRoleServiceHTTPServer(srv, roleService)
	adminV1.RegisterPositionServiceHTTPServer(srv, positionService)
	adminV1.RegisterTenantServiceHTTPServer(srv, tenantService)
	adminV1.RegisterPlanServiceHTTPServer(srv, planService)
	adminV1.RegisterPlanQuotaServiceHTTPServer(srv, planQuotaService)
	adminV1.RegisterPlanModuleServiceHTTPServer(srv, planModuleService)

	adminV1.RegisterLoginAuditLogServiceHTTPServer(srv, loginAuditLogService)
	adminV1.RegisterOperationAuditLogServiceHTTPServer(srv, operationAuditLogService)
	adminV1.RegisterRedisCacheMonitorServiceHTTPServer(srv, redisCacheMonitorService)
	adminV1.RegisterServerMonitorServiceHTTPServer(srv, serverMonitorService)
	adminV1.RegisterOnlineSessionServiceHTTPServer(srv, onlineSessionService)
	adminV1.RegisterDashboardServiceHTTPServer(srv, dashboardService)

	adminV1.RegisterFileServiceHTTPServer(srv, fileService)

	// 注册文件传输服务，用于处理文件上传下载等功能
	// TODO 它不能够使用代码生成器生成的Handler，需要手动注册。代码生成器生成的Handler无法处理文件上传下载的请求。
	// 但，代码生成器生成代码可以提供给OpenAPI使用。
	registerFileTransferServiceHandler(srv, fileTransferService)

	// 审计日志服务端导出：手动注册（二进制文件响应，proto 生成路由写不了响应头）。
	// 手动路由无 Operation → auth 中间件不应用 → 鉴权在 handler 内显式做（平台管理员）。
	srv.Route("/").GET("admin/v1/audit-logs:export", func(ctx http.Context) error {
		return auditExportService.ServeExport(ctx.Response(), ctx.Request())
	})

	adminV1.RegisterInternalMessageServiceHTTPServer(srv, internalMessageService)
	adminV1.RegisterInternalMessageCategoryServiceHTTPServer(srv, internalMessageCategoryService)
	adminV1.RegisterInternalMessageRecipientServiceHTTPServer(srv, internalMessageRecipientService)

	// register:route ── 新模块路由在此行后注册(make register 工具锚点,勿删)
	adminV1.RegisterMyTenantUsageServiceHTTPServer(srv, myTenantUsageService)
	adminV1.RegisterTaskMonitorServiceHTTPServer(srv, taskMonitorService)
	adminV1.RegisterConfigServiceHTTPServer(srv, configService)

	if cfg.GetServer().GetRest().GetEnableSwagger() {
		swaggerUI.RegisterSwaggerUIServerWithOption(
			srv,
			swaggerUI.WithTitle("GoWind Admin"),
			swaggerUI.WithMemoryData(assets.OpenApiData, "yaml"),
		)
	}

	if authorizer != nil {
		if err = authorizer.ResetPolicies(viewer.WithSystemContext(ctx.Context())); err != nil {
			log.Errorf("reset policies error: %v", err)
		}
	}

	return srv, nil
}

// userServiceServerAdapter 将 UserServiceHTTPServer 桥接为 UserServiceServer，
// 使其可以被 RedactedUserServiceServer 包装以实现 HTTP 路径的脱敏。
type userServiceServerAdapter struct {
	adminV1.UnsafeUserServiceServer
	adminV1.UserServiceHTTPServer
}
