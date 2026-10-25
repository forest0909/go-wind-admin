package main

import (
	"github.com/go-kratos/kratos/v2"
	"github.com/tx7do/go-utils/geoip/geolite"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/tx7do/go-utils/authorizer"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/channel"
	"go-wind-admin/app/admin/service/internal/server"
	"go-wind-admin/app/admin/service/internal/service"
)

// initApp 手写装配整个应用,是依赖注入点。
// initApp assembles the whole application by hand.
//
// 装配严格单向分层,自上而下的阅读顺序即依赖方向:
// The wiring is strictly layered; reading top-down follows the dependency direction:
//
//	基础设施 → 仓储层(data) → 认证与鉴权 → 服务层(service) → 传输层(server)
//
// 约定 / Conventions:
//   - 新增仓储/服务:在对应分层小节追加一行构造,并传给下游消费者;漏接由编译器在调用处报错。
//     To add a repo/service: append one constructor line in its layer section, then pass it to
//     downstream consumers; a missing connection is a compile error at the call site.
//   - 持有 cleanup 的资源创建成功后立即注册进 cleanups;任何一步失败,rollback 逆序执行已注册
//     的清理(shutdown 与中途失败共用同一条 LIFO 路径)。
//     Resources owning a cleanup register it immediately; rollback runs them LIFO both on
//     mid-way failure and on shutdown.
//   - 本文件只做构造与传参,不写业务逻辑。
//     Construction and parameter passing only; no business logic in this file.
func initApp(ctx *bootstrap.Context) (*kratos.App, func(), error) {
	// cleanup 注册表:rollback 时逆序执行。
	// Cleanup registry; rollback runs entries in reverse order.
	var cleanups []func()
	rollback := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// ═══════════════════════ 一、基础设施 ═══════════════════════

	redisClient, cleanupRedis, err := data.NewRedisClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	cleanups = append(cleanups, cleanupRedis)

	entClient, cleanupEnt, err := data.NewEntClient(ctx)
	if err != nil {
		rollback()
		return nil, nil, err
	}
	cleanups = append(cleanups, cleanupEnt)

	minioClient := data.NewMinIoClient(ctx)

	// 认证基建:令牌缓存 → 认证器 → 访问令牌校验器。
	clientType := data.NewClientType()
	passwordCrypto := data.NewPasswordCrypto()
	captcha := data.NewCaptcha(redisClient)
	userTokenCache := data.NewUserTokenCache(ctx, redisClient)
	authenticator := data.NewAuthenticator(ctx, userTokenCache)
	accessTokenChecker := data.NewTokenChecker(ctx, authenticator, clientType)
	loginRateLimiter := data.NewLoginRateLimiter(ctx, redisClient)
	mfaChallengeCache := data.NewMfaChallengeCache(ctx, redisClient)
	ssoStateCache := data.NewSsoStateCache(ctx, redisClient)
	geoClient, _ := geolite.NewClient()
	vcodeCache := data.NewVCodeCache(ctx, redisClient)

	// ═══════════════════════ 二、仓储层(internal/data) ═══════════════════════

	// 身份与账号
	userRoleRepo := data.NewUserRoleRepo(ctx, entClient)
	userOrgUnitRepo := data.NewUserOrgUnitRepo(ctx, entClient)
	userPositionRepo := data.NewUserPositionRepo(ctx, entClient)
	membershipRoleRepo := data.NewMembershipRoleRepo(ctx, entClient)
	membershipPositionRepo := data.NewMembershipPositionRepo(ctx, entClient)
	membershipOrgUnitRepo := data.NewMembershipOrgUnitRepo(ctx, entClient)
	membershipRepo := data.NewMembershipRepo(ctx, entClient, membershipRoleRepo, membershipPositionRepo, membershipOrgUnitRepo)
	userRepo := data.NewUserRepo(ctx, entClient, userRoleRepo, userOrgUnitRepo, userPositionRepo, membershipRepo)
	configRepo := data.NewConfigRepo(ctx, entClient, redisClient)
	userCredentialRepo := data.NewUserCredentialRepo(ctx, entClient, passwordCrypto, configRepo)
	userMfaFactorRepo := data.NewUserMfaFactorRepo(ctx, entClient)
	loginPolicyRepo := data.NewLoginPolicyRepo(ctx, entClient)

	// 组织架构与租户
	orgUnitRepo := data.NewOrgUnitRepo(ctx, entClient, userOrgUnitRepo)
	positionRepo := data.NewPositionRepo(ctx, entClient)
	tenantRepo := data.NewTenantRepo(ctx, entClient)
	tenantUsageRepo := data.NewTenantUsageRepo(ctx, entClient, authenticator)

	// RBAC:权限 → 角色(聚合权限),Api/Menu 为权限挂载点
	permissionApiRepo := data.NewPermissionApiRepo(ctx, entClient)
	permissionMenuRepo := data.NewPermissionMenuRepo(ctx, entClient)
	rolePermissionRepo := data.NewRolePermissionRepo(ctx, entClient)
	roleOrgUnitRepo := data.NewRoleOrgUnitRepo(ctx, entClient)
	roleFieldPermissionRepo := data.NewRoleFieldPermissionRepo(ctx, entClient)
	roleMetadataRepo := data.NewRoleMetadataRepo(ctx, entClient)
	permissionRepo := data.NewPermissionRepo(ctx, entClient, permissionApiRepo, permissionMenuRepo)
	roleRepo := data.NewRoleRepo(ctx, entClient, rolePermissionRepo, roleOrgUnitRepo, permissionRepo, roleMetadataRepo, roleFieldPermissionRepo)
	permissionGroupRepo := data.NewPermissionGroupRepo(ctx, entClient)
	apiRepo := data.NewApiRepo(ctx, entClient)
	menuRepo := data.NewMenuRepo(ctx, entClient)

	// 审计日志
	apiAuditLogRepo := data.NewApiAuditLogRepo(ctx, entClient)
	loginAuditLogRepo := data.NewLoginAuditLogRepo(ctx, entClient)
	operationAuditLogRepo := data.NewOperationAuditLogRepo(ctx, entClient)
	permissionAuditLogRepo := data.NewPermissionAuditLogRepo(ctx, entClient)
	dataAccessAuditLogRepo := data.NewDataAccessAuditLogRepo(ctx, entClient)
	policyEvaluationLogRepo := data.NewPolicyEvaluationLogRepo(ctx, entClient)
	auditLogArchiveRepo := data.NewAuditLogArchiveRepo(ctx, entClient)

	// 字典与多语言
	dictTypeRepo := data.NewDictTypeRepo(ctx, entClient)
	dictEntryI18nRepo := data.NewDictEntryI18nRepo(ctx, entClient)
	dictEntryRepo := data.NewDictEntryRepo(ctx, entClient, dictEntryI18nRepo)
	languageRepo := data.NewLanguageRepo(ctx, entClient)

	// 套餐与计费
	planRepo := data.NewPlanRepo(ctx, entClient)
	planQuotaRepo := data.NewPlanQuotaRepo(ctx, entClient)
	planModuleRepo := data.NewPlanModuleRepo(ctx, entClient)

	// 任务 / 文件 / 运维观测
	taskRepo := data.NewTaskRepo(ctx, entClient)
	fileRepo := data.NewFileRepo(ctx, entClient)
	redisCacheMonitorRepo := data.NewRedisCacheMonitorRepo(ctx, redisClient)
	serverMonitorRepo := data.NewServerMonitorRepo(ctx, entClient)
	notificationChannelRepo := data.NewNotificationChannelRepo(ctx, entClient)
	notificationDeliveryRepo := data.NewNotificationDeliveryRepo(ctx, entClient)
	dashboardRepo := data.NewDashboardRepo(ctx, entClient)

	// 站内信
	internalMessageRepo := data.NewInternalMessageRepo(ctx, entClient)
	internalMessageCategoryRepo := data.NewInternalMessageCategoryRepo(ctx, entClient)
	internalMessageRecipientRepo := data.NewInternalMessageRecipientRepo(ctx, entClient)
	notificationPreferenceRepo := data.NewNotificationPreferenceRepo(ctx, entClient)

	// ── register:repo ── 新模块仓储在此行后注册(make register 工具锚点,勿删)
	notificationRuleRepo := data.NewNotificationRuleRepo(ctx, entClient)
	notificationTemplateRepo := data.NewNotificationTemplateRepo(ctx, entClient)
	transactionalMailer := &service.TransactionMailer{TemplateRepo: notificationTemplateRepo}
	myTenantUsageService := service.NewMyTenantUsageService(ctx, tenantUsageRepo)
	accessKeyRepo := data.NewAccessKeyRepo(ctx, entClient)

	// ═══════════════════════ 三、认证与鉴权 ═══════════════════════

	tenantAccessChecker := data.NewTenantAccessCheckerImpl(ctx, entClient)
	authorizerProvider := data.NewAuthorizerProvider(ctx, roleRepo, apiRepo)
	var authzEngineCfg *authorizer.EngineConfig
	if bootstrapConf := ctx.GetConfig(); bootstrapConf != nil && bootstrapConf.Authz != nil {
		authzEngineCfg = &authorizer.EngineConfig{Type: bootstrapConf.Authz.GetType()}
	}
	authz := authorizer.NewAuthorizer(ctx.Context(), ctx.GetLogger(), authzEngineCfg, authorizerProvider)

	// ═══════════════════════ 四、服务层(internal/service) ═══════════════════════

	// 通知投递：渠道注册表（EMAIL / WEBHOOK / SMS / INTERNAL）→ NotificationService → 各业务 service 只拿 Notifier。
	// 注册顺序即装配位置：新增一种渠道 = 在这里多 Register 一个 Sender，业务调用方不动。
	// 路由（事件 → 渠道 + 同步/异步）读 sys_notification_rules，不在这里登记，见 resolveRoute。
	channelRegistry := channel.NewRegistry()
	channelRegistry.Register(channel.NewEmailSender(notificationChannelRepo))
	channelRegistry.Register(channel.NewWebhookSender(notificationChannelRepo))
	channelRegistry.Register(channel.NewSmsSender(notificationChannelRepo))
	channelRegistry.Register(channel.NewSettingsSender(notificationChannelRepo))
	notificationService := service.NewNotificationService(ctx, notificationDeliveryRepo, notificationRuleRepo, notificationTemplateRepo, channelRegistry)
	notificationTemplateService := service.NewNotificationTemplateService(ctx, notificationTemplateRepo)

	// 审计日志服务端导出（XLSX/CSV）：手动注册二进制响应路由，见 rest_server 的 registerFileTransfer 同段
	auditExportService := service.NewAuditExportService(ctx, loginAuditLogRepo, apiAuditLogRepo, operationAuditLogRepo, dataAccessAuditLogRepo, permissionAuditLogRepo, policyEvaluationLogRepo, accessTokenChecker)
	// 非审计数据服务端导出（AI 用量流水租户视角 / 通知台账平台闸），同段手动路由
	dataExportService := service.NewDataExportService(ctx, notificationDeliveryRepo, accessTokenChecker)
	// 系统级常驻任务监控（只读 asynq Inspector）
	taskMonitorService := service.NewTaskMonitorService(ctx)

	// 认证与登录策略
	authenticationService := service.NewAuthenticationService(ctx, userRepo, userCredentialRepo, roleRepo, tenantRepo, membershipRepo, orgUnitRepo, roleOrgUnitRepo, roleFieldPermissionRepo, permissionRepo, authenticator, clientType, captcha, loginRateLimiter, loginPolicyRepo, userMfaFactorRepo, mfaChallengeCache, ssoStateCache, geoClient, transactionalMailer, vcodeCache, notificationService)
	mfaService := service.NewMfaService(ctx, userMfaFactorRepo, mfaChallengeCache, authenticator, loginRateLimiter, userRepo)
	loginPolicyService := service.NewLoginPolicyService(ctx, loginPolicyRepo)

	// 身份与组织
	userService := service.NewUserService(ctx, userRepo, roleRepo, userCredentialRepo, positionRepo, orgUnitRepo, tenantRepo, tenantUsageRepo, membershipRepo, authenticator)
	userProfileService := service.NewUserProfileService(ctx, userRepo, roleRepo, userCredentialRepo, authenticator, notificationService, vcodeCache, minioClient, transactionalMailer)
	positionService := service.NewPositionService(ctx, positionRepo, orgUnitRepo)
	orgUnitService := service.NewOrgUnitService(ctx, orgUnitRepo, userRepo)

	// RBAC
	roleService := service.NewRoleService(ctx, authz, roleRepo, tenantRepo)
	menuService := service.NewMenuService(ctx, menuRepo)
	apiService := service.NewApiService(ctx, apiRepo, authz)
	permissionService := service.NewPermissionService(ctx, permissionRepo, permissionGroupRepo, menuRepo, apiRepo, roleRepo, authz)
	permissionGroupService := service.NewPermissionGroupService(ctx, permissionGroupRepo, permissionRepo)

	// 租户与套餐
	tenantService := service.NewTenantService(ctx, tenantRepo, tenantUsageRepo, userRepo, userCredentialRepo, roleRepo, authz)
	planService := service.NewPlanService(ctx, planRepo)
	planQuotaService := service.NewPlanQuotaService(ctx, planQuotaRepo)
	planModuleService := service.NewPlanModuleService(ctx, planModuleRepo)

	// 字典与多语言
	dictTypeService := service.NewDictTypeService(ctx, dictTypeRepo)
	dictEntryService := service.NewDictEntryService(ctx, dictEntryRepo)
	languageService := service.NewLanguageService(ctx, languageRepo)

	// 文件与任务
	fileService := service.NewFileService(ctx, fileRepo, minioClient)
	fileTransferService := service.NewFileTransferService(ctx, minioClient, fileRepo, tenantUsageRepo)
	taskService := service.NewTaskService(ctx, taskRepo, userRepo, tenantUsageRepo, auditLogArchiveRepo)

	// 审计日志
	loginAuditLogService := service.NewLoginAuditLogService(ctx, loginAuditLogRepo)
	apiAuditLogService := service.NewApiAuditLogService(ctx, apiAuditLogRepo, apiRepo)
	operationAuditLogService := service.NewOperationAuditLogService(ctx, operationAuditLogRepo)
	dataAccessAuditLogService := service.NewDataAccessAuditLogService(ctx, dataAccessAuditLogRepo)
	permissionAuditLogService := service.NewPermissionAuditLogService(ctx, permissionAuditLogRepo)
	policyEvaluationLogService := service.NewPolicyEvaluationLogService(ctx, policyEvaluationLogRepo)

	// 运维观测与门户
	redisCacheMonitorService := service.NewRedisCacheMonitorService(ctx, redisCacheMonitorRepo)
	serverMonitorService := service.NewServerMonitorService(ctx, serverMonitorRepo)
	notificationChannelService := service.NewNotificationChannelService(ctx, notificationChannelRepo, notificationService, transactionalMailer)
	onlineSessionService := service.NewOnlineSessionService(ctx, authenticator)
	adminPortalService := service.NewAdminPortalService(ctx, menuRepo, roleRepo, userRepo, permissionRepo, planModuleRepo, tenantRepo)

	// 站内信
	internalMessageService := service.NewInternalMessageService(ctx, internalMessageRepo, internalMessageCategoryRepo, internalMessageRecipientRepo, userRepo, notificationPreferenceRepo, authenticator, clientType)
	internalMessageCategoryService := service.NewInternalMessageCategoryService(ctx, internalMessageCategoryRepo)
	internalMessageRecipientService := service.NewInternalMessageRecipientService(ctx, internalMessageRepo, internalMessageRecipientRepo)
	notificationPreferenceService := service.NewNotificationPreferenceService(ctx, notificationPreferenceRepo, internalMessageCategoryRepo)

	// 站内信 ⇄ 通知域接线（两条边互为依赖，只能装配期后贴）：
	//   NotificationService --Registry--> InternalMessageSender --> InternalMessageService（投递内核）
	//   InternalMessageService --Notifier--> NotificationService（缝的入口）
	// 注册放在 internalMessageService 之后是硬要求：Sender 持有服务对象本身，才能在
	// RegisterInternalMessagePublisher（SSE 启动时）之后读到活的 publisher 而不是构造期快照。
	channelRegistry.Register(service.NewInternalMessageSender(internalMessageService))
	internalMessageService.RegisterNotifier(notificationService)

	dashboardService := service.NewDashboardService(ctx, dashboardRepo)

	// 套餐配额水位通知：用量扫描（tenantUsageRepo）+ 站内信投递内核。
	// 依赖 internalMessageService，故置其后。
	planQuotaWatermarkService := service.NewPlanQuotaWatermarkService(ctx, tenantUsageRepo, internalMessageService, internalMessageRepo, entClient)

	// ── register:service ── 新模块服务在此行后注册(make register 工具锚点,勿删)
	notificationRuleService := service.NewNotificationRuleService(ctx, notificationRuleRepo, notificationChannelRepo, notificationService)
	accessKeyService := service.NewAccessKeyService(ctx, accessKeyRepo, authenticator, loginRateLimiter)
	configService := service.NewConfigService(ctx, configRepo)

	// ═══════════════════════ 五、传输层(internal/server) ═══════════════════════

	restMiddlewares := server.NewRestMiddleware(ctx, accessTokenChecker, tenantAccessChecker, authz,
		apiAuditLogRepo, loginAuditLogRepo, operationAuditLogRepo, permissionAuditLogRepo, dataAccessAuditLogRepo, policyEvaluationLogRepo)

	restServer, err := server.NewRestServer(ctx, restMiddlewares, authz,
		authenticationService, mfaService, loginPolicyService,
		adminPortalService, taskService,
		fileService, fileTransferService,
		dictTypeService, dictEntryService, languageService,
		tenantService, planService, planQuotaService, planModuleService,
		userService, userProfileService, roleService, positionService, orgUnitService,
		menuService, apiService, permissionService, permissionGroupService,
		permissionAuditLogService, policyEvaluationLogService,
		loginAuditLogService, apiAuditLogService, operationAuditLogService, dataAccessAuditLogService,
		redisCacheMonitorService, serverMonitorService, notificationChannelService,
		notificationService, onlineSessionService, dashboardService,
		internalMessageService, internalMessageCategoryService, internalMessageRecipientService,
		// register:rest-arg ── 新模块服务实参在此行后追加(make register 工具锚点,勿删)
		notificationRuleService,
		notificationPreferenceService,
		notificationTemplateService,
		myTenantUsageService,
		auditExportService,
		dataExportService,
		taskMonitorService,
		accessKeyService,
		configService,
	)
	if err != nil {
		rollback()
		return nil, nil, err
	}

	asynqServer, err := server.NewAsynqServer(ctx, taskService, internalMessageService, notificationService, planQuotaWatermarkService)
	if err != nil {
		rollback()
		return nil, nil, err
	}

	sseServer := server.NewSseServer(ctx, internalMessageService)

	return newApp(ctx, restServer, asynqServer, sseServer), rollback, nil
}
