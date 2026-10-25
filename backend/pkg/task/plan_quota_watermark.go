package task

// PlanQuotaWatermarkTaskType 是套餐配额水位扫描任务的类型常量。
// 系统级常驻定时任务：每日扫描全部 ON 租户的三类套餐配额（USER_LIMIT / STORAGE /
// API_CALL）用量水位，对达到阈值（见 plan_billing.md §7.3）的
// 租户，按其管理员偏好语言渲染站内信告警并投递给租户管理员。
// handler 为 PlanQuotaWatermarkService.AsyncPlanQuotaWatermarkScan。
const PlanQuotaWatermarkTaskType = "plan_quota_watermark_scan"

// PlanQuotaWatermarkCronSpec 水位扫描的 cron（每天 09:00）。
const PlanQuotaWatermarkCronSpec = "0 9 * * *"

// PlanQuotaWatermarkTaskData 水位扫描任务载荷（无参数：扫描对象为全部 ON 租户）。
type PlanQuotaWatermarkTaskData struct{}
