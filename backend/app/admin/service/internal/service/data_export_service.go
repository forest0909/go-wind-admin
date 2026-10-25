package service

import (
	"net/http"
	"strconv"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	notificationV1 "go-wind-admin/api/gen/go/notification/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
)

// 非审计数据的服务端导出单次行数上限（与审计导出同额：防无界查询 OOM，
// 见 audit_export_service.go 的说明；更大规模走各自的归档通道）。
const dataExportMaxRows = 500000

// DataExportService 非审计数据的服务端导出（XLSX/CSV，手动路由 + 公共内核）。
//
// 权限语义与列表接口对齐：
//   - 通知投递台账：平台管理员专属（表无 tenant_id，租户可见即全平台可见，
//     语义对齐 requirePlatformAdmin，见 notification_platform_guard.go）。
type DataExportService struct {
	deliveryRepo *data.NotificationDeliveryRepo
	log          *bLogger.Helper

	// tokenChecker 手动路由的鉴权锚：:export 不穿 kratos middleware，
	// auth 中间件的一套在此补齐（见 export_common.go 公共样板）。
	tokenChecker auth.AccessTokenChecker
}

func NewDataExportService(
	ctx *bootstrap.Context,
	deliveryRepo *data.NotificationDeliveryRepo,
	tokenChecker auth.AccessTokenChecker,
) *DataExportService {
	return &DataExportService{
		deliveryRepo: deliveryRepo,
		log:          ctx.NewLoggerHelper("data-export/service/admin-service"),
		tokenChecker: tokenChecker,
	}
}

// ServeNotificationDeliveryExport GET /admin/v1/notification-deliveries:export
// —— 通知投递台账导出（平台管理员专属）。
func (s *DataExportService) ServeNotificationDeliveryExport(w http.ResponseWriter, r *http.Request) error {
	ctx, operator, ok := authorizeExportRequest(w, r, s.tokenChecker)
	if !ok {
		return nil
	}
	if !requireExportPlatformAdmin(w, operator) {
		return nil
	}

	format, req, pok := parseExportRequest(w, r, dataExportMaxRows)
	if !pok {
		return nil
	}

	resp, listErr := s.deliveryRepo.List(ctx, req)
	if listErr != nil {
		s.log.Errorf(ctx, "export [notification_delivery] query failed: %s", listErr.Error())
		http.Error(w, "query failed", http.StatusInternalServerError)
		return nil
	}

	cols := notificationDeliveryExportColumns()
	headers := colHeaders(cols)
	var rows [][]string
	for _, row := range resp.GetItems() {
		rows = append(rows, colValues(cols, row))
	}

	emitExportFile(w, s.log, ctx, "notification_delivery", "notification-deliveries", "notification_delivery", headers, rows, format)
	return nil
}

// ==== 列定义（表头用英文字段名：导出文件没有运行时语言上下文）====

// notificationDeliveryExportColumns 与投递台账页展示列一致。
func notificationDeliveryExportColumns() []exportColumn[*notificationV1.NotificationDelivery] {
	return []exportColumn[*notificationV1.NotificationDelivery]{
		{header: "createdAt", value: func(r *notificationV1.NotificationDelivery) string { return fmtTime(r.GetCreatedAt()) }},
		{header: "sentAt", value: func(r *notificationV1.NotificationDelivery) string { return fmtTime(r.GetSentAt()) }},
		{header: "eventType", value: func(r *notificationV1.NotificationDelivery) string { return r.GetEventType().String() }},
		{header: "channel", value: func(r *notificationV1.NotificationDelivery) string { return r.GetChannel().String() }},
		{header: "status", value: func(r *notificationV1.NotificationDelivery) string { return r.GetStatus().String() }},
		{header: "target", value: func(r *notificationV1.NotificationDelivery) string { return r.GetTarget() }},
		{header: "attempts", value: func(r *notificationV1.NotificationDelivery) string {
			return strconv.FormatUint(uint64(r.GetAttempts()), 10)
		}},
		{header: "recipientUserId", value: func(r *notificationV1.NotificationDelivery) string {
			return strconv.FormatUint(uint64(r.GetRecipientUserId()), 10)
		}},
		{header: "channelId", value: func(r *notificationV1.NotificationDelivery) string {
			return strconv.FormatUint(uint64(r.GetChannelId()), 10)
		}},
		{header: "relatedId", value: func(r *notificationV1.NotificationDelivery) string {
			return strconv.FormatUint(uint64(r.GetRelatedId()), 10)
		}},
		{header: "requestId", value: func(r *notificationV1.NotificationDelivery) string { return r.GetRequestId() }},
		{header: "lastError", value: func(r *notificationV1.NotificationDelivery) string { return r.GetLastError() }},
	}
}
