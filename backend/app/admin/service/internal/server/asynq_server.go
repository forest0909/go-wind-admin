package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bootstrapAsynq "go-wind-admin/third_party/kratos-bootstrap/transport/asynq"
	asynqServer "go-wind-admin/third_party/kratos-transport/transport/asynq"

	"go-wind-admin/app/admin/service/internal/service"

	"go-wind-admin/pkg/task"
	"go-wind-admin/third_party/go-crud/viewer"
)

// NewAsynqServer creates a new asynq server.
func NewAsynqServer(ctx *bootstrap.Context, taskService *service.TaskService, internalMessageService *service.InternalMessageService) (*asynqServer.Server, error) {
	cfg := ctx.GetConfig()

	if cfg == nil || cfg.Server == nil || cfg.Server.Asynq == nil {
		return nil, nil
	}

	srv := bootstrapAsynq.NewAsynqServer(
		cfg.Server.Asynq,
		asynqServer.WithEnableKeepAlive(false),
	)

	taskService.RegisterTaskScheduler(srv)
	// 注入 asynq 任务入队能力，使广播 fan-out 改走 asynq 任务（可重试、断点恢复）。
	// asynq 未配置时本函数在上方 return nil，此行不会执行，internalMessageService.taskEnqueuer 保持 nil。
	internalMessageService.RegisterTaskEnqueuer(taskService)

	var err error

	// 注册审计日志归档任务 handler（等保 ≥6 个月留存：超期行导出 JSONL 后删除）。
	// 周期调度在 startAllTask 末尾注册，以确保 RestartAllTask
	// （先 RemoveAllPeriodicTask 再 startAllTask）后仍能恢复。
	if err = asynqServer.RegisterSubscriber(srv, task.AuditLogArchiveTaskType, taskService.AsyncAuditLogArchive); err != nil {
		log.Error(err)
		return nil, err
	}

	// 注册站内信广播任务 handler。该任务为一次性投递任务（非周期），
	// 由 InternalMessageService.SendMessage 在消息落库后入队，handler 从 DB 取回消息本体后执行 fan-out。
	// 重试幂等性由 (message_id, recipient_user_id) 唯一约束 + CreateBulk 的 ON CONFLICT DO NOTHING 保证。
	if err = asynqServer.RegisterSubscriber(srv, task.BroadcastMessageTaskType, internalMessageService.AsyncBroadcastMessage); err != nil {
		log.Error(err)
		return nil, err
	}

	// 启动所有的任务
	if _, err = taskService.StartAllTask(viewer.WithSystemContext(ctx.Context()), &emptypb.Empty{}); err != nil {
		log.Error(err)
		return nil, err
	}

	return srv, nil
}
