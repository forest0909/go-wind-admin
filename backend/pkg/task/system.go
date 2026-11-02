package task

import "time"

// systemTaskTypes 由系统自己投递或调度的任务类型。它们的 handler 信任 payload（例如广播任务
// 按 message_id 向全员扇出），所以不能经 TaskService（sys_tasks 表）创建、修改或启停，
// 也不出现在 ListTaskTypeName 里。新增系统任务类型时在这里登记。
var systemTaskTypes = map[string]struct{}{
	BroadcastMessageTaskType: {},
	AuditLogArchiveTaskType:  {},
}

// IsSystemTaskType 判断 typeName 是否为系统任务类型。
func IsSystemTaskType(typeName string) bool {
	_, ok := systemTaskTypes[typeName]
	return ok
}

// AuditLogArchiveOnceWindow 多副本时每个实例的 cron 都会入队同一轮归档。
// 入队用 asynq.Unique 去重，执行时再按这个时间窗加 Redis 标记，同一窗口内只有一个实例真正执行。
// 必须明显短于 AuditLogArchiveCronSpec 的周期（每天一次）。
const AuditLogArchiveOnceWindow = time.Hour
