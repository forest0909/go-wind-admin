package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/third_party/go-utils/trans"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	taskV1 "go-wind-admin/api/gen/go/task/service/v1"

	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/redissync"
	"go-wind-admin/pkg/serviceid"
	"go-wind-admin/pkg/task"
	"go-wind-admin/third_party/go-crud/viewer"
)

// TaskScheduler 任务调度接口
type TaskScheduler interface {
	TaskTypeExists(taskType string) bool
	GetRegisteredTaskTypes() []string

	NewTask(typeName string, msg any, opts ...asynq.Option) error
	NewWaitResultTask(typeName string, msg any, opts ...asynq.Option) error
	NewPeriodicTask(cronSpec, typeName string, msg any, opts ...asynq.Option) (string, error)

	// RemovePeriodicTaskByID 按 NewPeriodicTask 返回的 entryID 注销调度项。
	// 不用按 typeName 注销的 RemovePeriodicTask / 全量注销的 RemoveAllPeriodicTask：
	// 前者会误删同名的系统调度项，后者会连系统调度项一起清掉。
	RemovePeriodicTaskByID(entryID string) error
}

// TaskService 任务服务
type TaskService struct {
	adminV1.TaskServiceHTTPServer

	log *bLogger.Helper

	taskScheduler TaskScheduler

	userRepo            data.UserRepo
	taskRepo            *data.TaskRepo
	auditLogArchiveRepo *data.AuditLogArchiveRepo

	// rdb 用于系统周期任务的多副本执行去重（见 AsyncAuditLogArchive），为 nil 时不去重。
	rdb redis.UniversalClient

	// 本实例注册的周期调度项。用户任务按任务行 ID 绑定，系统任务按 typeName 绑定，
	// 两者互不影响：停止/删除用户任务只注销它自己的 entry。
	entryMu       sync.Mutex
	taskEntries   map[uint32]string
	systemEntries map[string]string
}

func NewTaskService(
	ctx *bootstrap.Context,
	taskRepo *data.TaskRepo,
	userRepo data.UserRepo,
	auditLogArchiveRepo *data.AuditLogArchiveRepo,
	rdb *redis.Client,
) *TaskService {
	svc := &TaskService{
		log:                 ctx.NewLoggerHelper("task/service/admin-service"),
		taskRepo:            taskRepo,
		userRepo:            userRepo,
		auditLogArchiveRepo: auditLogArchiveRepo,
	}
	if rdb != nil { // 避免把 nil *redis.Client 存成非 nil 接口
		svc.rdb = rdb
	}

	return svc
}

func (s *TaskService) RegisterTaskScheduler(taskScheduler TaskScheduler) {
	s.taskScheduler = taskScheduler
}

// NewTask 委托 taskScheduler.NewTask 投递一次性 asynq 任务。
// 实现 service.TaskEnqueuer 接口，供 InternalMessageService 广播任务入队使用。
// asynq 未配置时 taskScheduler 为 nil，调用方（InternalMessageService）通过 nil 检查回退到 goroutine。
func (s *TaskService) NewTask(typeName string, msg any, opts ...asynq.Option) error {
	if !s.hasScheduler() {
		return errors.New("task scheduler is not configured")
	}
	return s.taskScheduler.NewTask(typeName, msg, opts...)
}

// hasScheduler 检查调度器是否可用（未配置 asynq 时为 nil）。
// 在所有调用 taskScheduler 的地方前置检查，避免 nil 解引用 panic。
func (s *TaskService) hasScheduler() bool {
	return s.taskScheduler != nil
}

func (s *TaskService) List(ctx context.Context, req *paginationV1.PagingRequest) (*taskV1.ListTaskResponse, error) {
	return s.taskRepo.List(ctx, req)
}

func (s *TaskService) Get(ctx context.Context, req *taskV1.GetTaskRequest) (*taskV1.Task, error) {
	return s.taskRepo.Get(ctx, req)
}

func (s *TaskService) ListTaskTypeName(_ context.Context, _ *emptypb.Empty) (*taskV1.ListTaskTypeNameResponse, error) {
	// nil 调度器防护：asynq 未配置时返回空列表而非 panic
	if !s.hasScheduler() {
		return &taskV1.ListTaskTypeNameResponse{}, nil
	}
	var typeNames []string
	for _, name := range s.taskScheduler.GetRegisteredTaskTypes() {
		if !task.IsSystemTaskType(name) {
			typeNames = append(typeNames, name)
		}
	}
	return &taskV1.ListTaskTypeNameResponse{
		TypeNames: typeNames,
	}, nil
}

func (s *TaskService) Create(ctx context.Context, req *taskV1.CreateTaskRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	if err := rejectSystemTaskType(req.Data.GetTypeName()); err != nil {
		return nil, err
	}

	// H8：校验 typeName 已在调度器注册，防止创建无 handler 的幽灵任务（每个 cron tick 报错）
	if !s.hasScheduler() || !s.taskScheduler.TaskTypeExists(req.Data.GetTypeName()) {
		return nil, adminV1.ErrorBadRequest("task type [%s] is not registered", req.Data.GetTypeName())
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	var t *taskV1.Task
	if t, err = s.taskRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	// 禁用状态的任务只需落库，不进入调度器
	if !t.GetEnable() {
		return &emptypb.Empty{}, nil
	}

	if err = s.startTask(t); err != nil {
		// 调度失败不掩盖：DB 记录已建，但任务实际不会运行，需明确告知
		s.log.Errorf(ctx, "create task [%s] succeeded but scheduling failed: %s", t.GetTypeName(), err.Error())
		return nil, adminV1.ErrorInternalServerError("task scheduling failed: %s", err.Error())
	}

	return &emptypb.Empty{}, nil
}

func (s *TaskService) Update(ctx context.Context, req *taskV1.UpdateTaskRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取更新前的任务，用于判断调度器中是否有正在运行的注册项。
	// 此处不可吞错：若 Get 失败则 oldTask==nil，下方 remove 会因 oldTask==nil 被跳过，
	// 导致旧调度项残留（任务"停用/更新后仍运行"）。故失败直接返回。
	oldTask, err := s.taskRepo.Get(ctx, &taskV1.GetTaskRequest{QueryBy: &taskV1.GetTaskRequest_Id{Id: req.GetId()}})
	if err != nil {
		return nil, err
	}

	// H8：校验 typeName 已在调度器注册。
	// 列表行的启停开关只提交 {enable}，不带 typeName；此时从库中回填原值再校验，
	// 否则开关永远 400 "task type [] is not registered"。
	if req.Data.GetTypeName() == "" && oldTask != nil {
		req.Data.TypeName = trans.Ptr(oldTask.GetTypeName())
	}
	// 新旧 typeName 都不能是系统类型：既不能把用户任务改成系统任务，也不能改动遗留的系统类型行。
	if err := rejectSystemTaskType(oldTask.GetTypeName()); err != nil {
		return nil, err
	}
	if err := rejectSystemTaskType(req.Data.GetTypeName()); err != nil {
		return nil, err
	}
	if !s.hasScheduler() || !s.taskScheduler.TaskTypeExists(req.Data.GetTypeName()) {
		return nil, adminV1.ErrorBadRequest("task type [%s] is not registered", req.Data.GetTypeName())
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	var t *taskV1.Task
	if t, err = s.taskRepo.Update(ctx, req); err != nil {

		return nil, err
	}

	// 先移除本任务行旧的调度项（若存在），避免停用后仍运行、或启用时重复注册。
	// 绕过 stopTask 内部的 enable 保护：oldTask 可能已是禁用状态，但调度项仍可能残留。
	if _, removeErr := s.unscheduleTask(oldTask.GetId()); removeErr != nil {
		s.log.Warnf(ctx, "移除旧定时任务注册项失败[%d %s]: %v", oldTask.GetId(), oldTask.GetTypeName(), removeErr)
	}

	// 根据更新后的 enable 状态决定是否重新启动。
	// 注意：disable 也是一种合法的更新意图（更新即停用），不应视为调度失败。
	// startTask 内部对 enable==false 直接返回 error，若此处无差别调用会把
	// "成功停用"误报成 InternalServerError。因此仅在 enable 时才启动。
	if t.GetEnable() {
		if err = s.startTask(t); err != nil {
			s.log.Error(ctx, err.Error())
			return nil, adminV1.ErrorInternalServerError("task scheduling failed: %s", err.Error())
		}
	}

	return &emptypb.Empty{}, nil
}

func (s *TaskService) Delete(ctx context.Context, req *taskV1.DeleteTaskRequest) (*emptypb.Empty, error) {
	var err error
	var t *taskV1.Task
	// 获取待删除任务用于清理调度项。失败必须返回：否则 t==nil 会跳过调度清理，
	// 造成 DB 已删但调度器里周期任务仍触发的"幽灵任务"泄漏。
	if t, err = s.taskRepo.Get(ctx, &taskV1.GetTaskRequest{QueryBy: &taskV1.GetTaskRequest_Id{Id: req.GetId()}}); err != nil {
		return nil, err
	}

	if err = s.taskRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	// DB 删除成功后清理本任务行的调度项（绕过 stopTask 的 enable 保护：禁用任务也可能残留调度项）。
	// 只按任务行 ID 注销，不会碰到同名的系统调度项，所以遗留的系统类型行也允许删除。
	if _, removeErr := s.unscheduleTask(t.GetId()); removeErr != nil {
		// 注销失败仅告警，不阻断删除（DB 记录已删）
		s.log.Warnf(ctx, "删除任务后注销调度项失败[%d %s]: %v", t.GetId(), t.GetTypeName(), removeErr)
	}

	return &emptypb.Empty{}, nil
}

// ControlTask 控制调度任务
func (s *TaskService) ControlTask(ctx context.Context, req *taskV1.ControlTaskRequest) (*emptypb.Empty, error) {
	t, err := s.taskRepo.Get(ctx, &taskV1.GetTaskRequest{QueryBy: &taskV1.GetTaskRequest_TypeName{TypeName: req.GetTypeName()}})
	if err != nil {
		s.log.Errorf(ctx, "获取任务失败[%s]", err.Error())
		return nil, err
	}
	if err = rejectSystemTaskType(t.GetTypeName()); err != nil {
		return nil, err
	}

	switch req.GetControlType() {
	case taskV1.ControlTaskRequest_Restart:
		if err = s.stopTask(t); err != nil {
			return nil, err
		}

		if err = s.startTask(t); err != nil {
			return nil, err
		}

	case taskV1.ControlTaskRequest_Stop:
		err = s.stopTask(t)
		return nil, err

	case taskV1.ControlTaskRequest_Start:
		err = s.startTask(t)
		return nil, err

	default:
		// H8：未知控制类型必须明确拒绝，避免静默返回成功
		return nil, adminV1.ErrorBadRequest("invalid control type")
	}

	return &emptypb.Empty{}, nil
}

// StopAllTask 停止所有的用户调度任务（系统调度项不受影响）
func (s *TaskService) StopAllTask(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	s.stopAllTask()
	return &emptypb.Empty{}, nil
}

// StartAllTask 启动所有的调度任务
func (s *TaskService) StartAllTask(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	_, err := s.startAllTask(ctx)
	if err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// RestartAllTask 重启所有的用户调度任务（系统调度项不受影响）
func (s *TaskService) RestartAllTask(ctx context.Context, _ *emptypb.Empty) (*taskV1.RestartAllTaskResponse, error) {
	// 停止所有的用户任务
	s.stopAllTask()

	// 重新启动所有的任务
	count, err := s.startAllTask(ctx)

	return &taskV1.RestartAllTaskResponse{
		Count: count,
	}, err
}

// startAllTask 启动所有的任务
func (s *TaskService) startAllTask(ctx context.Context) (int32, error) {
	resp, err := s.List(ctx, &paginationV1.PagingRequest{
		NoPaging: trans.Ptr(true),
	})
	if err != nil {
		s.log.Errorf(ctx, "获取任务列表失败[%s]", err.Error())
		return 0, err
	}

	s.log.Infof(ctx, "开始开启定时任务，总计[%d]个", resp.GetTotal())

	// 调度项按任务行 ID 绑定（见 startTask），同名任务不再互相覆盖，无需按 typeName 去重。
	var count int32
	for _, t := range resp.GetItems() {
		if task.IsSystemTaskType(t.GetTypeName()) {
			// 旧版本允许经 TaskService 创建系统类型的任务行，这类行一律不调度
			s.log.Warnf(ctx, "跳过系统类型[%s]的任务行[%d]：系统任务不经任务表调度", t.GetTypeName(), t.GetId())
			continue
		}
		if s.startTask(t) != nil {
			continue
		}
		count++
	}

	s.log.Infof(ctx, "总共成功开启定时任务[%d]个", count)

	s.ensureSystemPeriodicTasks(ctx)

	return count, nil
}

// ensureSystemPeriodicTasks 注册系统级常驻周期任务（不依赖 sys_tasks 表）。
// 已注册的跳过，所以 StartAllTask / RestartAllTask 重复调用不会叠加调度项。
// handler 已在 NewAsynqServer 中通过 RegisterSubscriber 注册。
func (s *TaskService) ensureSystemPeriodicTasks(ctx context.Context) {
	if !s.hasScheduler() {
		return
	}

	s.entryMu.Lock()
	defer s.entryMu.Unlock()

	if _, ok := s.systemEntries[task.AuditLogArchiveTaskType]; ok {
		return
	}
	// 审计日志归档（等保 ≥6 个月留存）：超保留期行导出 JSONL 后删除，目录/保留期走环境变量。
	// 多副本时每个实例都会注册这个 cron；asynq.Unique 让同一轮只入队一次，
	// 执行端再用 Redis 标记兜底（见 AsyncAuditLogArchive）。
	entryID, err := s.taskScheduler.NewPeriodicTask(
		task.AuditLogArchiveCronSpec,
		task.AuditLogArchiveTaskType,
		&task.AuditLogArchiveTaskData{},
		asynq.Unique(task.AuditLogArchiveOnceWindow),
	)
	if err != nil {
		s.log.Errorf(ctx, "注册审计日志归档定时任务失败: %s", err.Error())
		return
	}
	if s.systemEntries == nil {
		s.systemEntries = make(map[string]string)
	}
	s.systemEntries[task.AuditLogArchiveTaskType] = entryID
	s.log.Infof(ctx, "审计日志归档定时任务已注册（cron=%s）", task.AuditLogArchiveCronSpec)
}

// stopAllTask 注销本实例上全部用户任务的调度项，系统调度项保留。
func (s *TaskService) stopAllTask() {
	// nil 调度器防护
	if !s.hasScheduler() {
		s.log.Warnf(context.Background(), "task scheduler is not configured, skip stopAllTask")
		return
	}

	s.log.Infof(context.Background(), "开始清除所有的用户定时任务...")

	s.entryMu.Lock()
	entries := s.taskEntries
	s.taskEntries = nil
	s.entryMu.Unlock()

	for taskID, entryID := range entries {
		if err := s.taskScheduler.RemovePeriodicTaskByID(entryID); err != nil {
			s.log.Errorf(context.Background(), "注销定时任务[%d]失败: %v", taskID, err)
		}
	}

	s.log.Infof(context.Background(), "完成清除所有的用户定时任务，共[%d]个", len(entries))
}

// unscheduleTask 注销任务行 taskID 在本实例的周期调度项。没有调度项时返回 false。
func (s *TaskService) unscheduleTask(taskID uint32) (bool, error) {
	if !s.hasScheduler() {
		return false, nil
	}
	s.entryMu.Lock()
	entryID, ok := s.taskEntries[taskID]
	delete(s.taskEntries, taskID)
	s.entryMu.Unlock()
	if !ok {
		return false, nil
	}
	return true, s.taskScheduler.RemovePeriodicTaskByID(entryID)
}

// rejectSystemTaskType 系统任务类型不允许经任务管理接口创建、修改或启停。
func rejectSystemTaskType(typeName string) error {
	if task.IsSystemTaskType(typeName) {
		return adminV1.ErrorForbidden("task type [%s] is reserved for the system", typeName)
	}
	return nil
}

// stopTask 停止一个任务
func (s *TaskService) stopTask(t *taskV1.Task) error {
	if t == nil {
		return errors.New("task is nil")
	}

	if !t.GetEnable() {
		return errors.New("task is not enable")
	}

	// nil 调度器防护
	if !s.hasScheduler() {
		return errors.New("task scheduler is not configured")
	}

	switch t.GetType() {
	case taskV1.Task_PERIODIC:
		removed, err := s.unscheduleTask(t.GetId())
		if err != nil {
			return err
		}
		if !removed {
			return errors.New("periodic task is not scheduled")
		}
		return nil

	// DELAY/WAIT_RESULT 是已投递到 asynq 队列的一次性任务，底层 transport
	// 未暴露 Inspector（删除排队任务/取消在途任务的能力），无法安全停止。
	// 此前空 case 静默返回成功，用户会以为已停止——必须诚实报错。
	case taskV1.Task_DELAY:
		return errors.New("queued one-shot task cannot be stopped, it will run at its scheduled time")
	case taskV1.Task_WAIT_RESULT:
		return errors.New("in-flight task cannot be stopped, please wait for it to finish")
	}

	return nil
}

// convertTaskOption 转换任务选项
func (s *TaskService) convertTaskOption(t *taskV1.Task) (opts []asynq.Option, payload any) {
	if t == nil {
		return
	}

	if len(t.GetTaskPayload()) > 0 {
		_ = json.Unmarshal([]byte(t.GetTaskPayload()), &payload)
	}

	// asynq 拒绝 nil message（"message is nil"）；任务数据留空时兜底为空对象
	if payload == nil {
		payload = map[string]any{}
	}

	if t.TaskOptions != nil {
		if t.GetTaskOptions().GetMaxRetry() > 0 {
			opts = append(opts, asynq.MaxRetry(int(t.GetTaskOptions().GetMaxRetry())))
		}
		if t.GetTaskOptions().Timeout != nil {
			opts = append(opts, asynq.Timeout(t.GetTaskOptions().GetTimeout().AsDuration()))
		}
		if t.GetTaskOptions().Deadline != nil {
			opts = append(opts, asynq.Deadline(t.GetTaskOptions().GetDeadline().AsTime()))
		}
		if t.GetTaskOptions().ProcessIn != nil {
			opts = append(opts, asynq.ProcessIn(t.GetTaskOptions().GetProcessIn().AsDuration()))
		}
		if t.GetTaskOptions().ProcessAt != nil {
			opts = append(opts, asynq.ProcessAt(t.GetTaskOptions().GetProcessAt().AsTime()))
		}
		if t.GetTaskOptions().UniqueTtl != nil {
			opts = append(opts, asynq.Unique(t.GetTaskOptions().GetUniqueTtl().AsDuration()))
		}
		if t.GetTaskOptions().Retention != nil {
			opts = append(opts, asynq.Retention(t.GetTaskOptions().GetRetention().AsDuration()))
		}
		opts = append(opts, asynq.Group(t.GetTaskOptions().GetGroup()))
		opts = append(opts, asynq.TaskID(t.GetTaskOptions().GetTaskId()))
	}

	return
}

// startTask 启动一个任务
func (s *TaskService) startTask(t *taskV1.Task) error {
	if t == nil {
		return errors.New("task is nil")
	}

	if !t.GetEnable() {
		return errors.New("task is not enable")
	}

	// nil 调度器防护
	if !s.hasScheduler() {
		return errors.New("task scheduler is not configured")
	}

	var opts []asynq.Option
	var payload any
	var err error

	switch t.GetType() {
	case taskV1.Task_PERIODIC:
		// 同一任务行重复启动时先注销旧调度项，避免一行任务挂两个 entry
		if _, err = s.unscheduleTask(t.GetId()); err != nil {
			s.log.Errorf(context.Background(), "[%d %s] 注销旧调度项失败[%s]", t.GetId(), t.GetTypeName(), err.Error())
			return err
		}
		opts, payload = s.convertTaskOption(t)
		var entryID string
		if entryID, err = s.taskScheduler.NewPeriodicTask(t.GetCronSpec(), t.GetTypeName(), payload, opts...); err != nil {
			s.log.Errorf(context.Background(), "[%s] 创建定时任务失败[%s]", t.GetTypeName(), err.Error())
			return err
		}
		s.entryMu.Lock()
		if s.taskEntries == nil {
			s.taskEntries = make(map[uint32]string)
		}
		s.taskEntries[t.GetId()] = entryID
		s.entryMu.Unlock()

	case taskV1.Task_DELAY:
		opts, payload = s.convertTaskOption(t)
		if err = s.taskScheduler.NewTask(t.GetTypeName(), payload, opts...); err != nil {
			s.log.Errorf(context.Background(), "[%s] 创建延迟任务失败[%s]", t.GetTypeName(), err.Error())
			return err
		}

	case taskV1.Task_WAIT_RESULT:
		opts, payload = s.convertTaskOption(t)
		if err = s.taskScheduler.NewWaitResultTask(t.GetTypeName(), payload, opts...); err != nil {
			s.log.Errorf(context.Background(), "[%s] 创建等待结果任务失败[%s]", t.GetTypeName(), err.Error())
			return err
		}
	}

	return nil
}

// AsyncAuditLogArchive 审计日志归档任务：把超过保留期的审计行导出为本地
// JSONL 文件后从库中删除（等保要求日志留存 ≥6 个月）。
// 目录：AUDIT_ARCHIVE_DIR（默认 ./data/audit-archive）；
// 保留天数：AUDIT_RETENTION_DAYS（默认 180）。
func (s *TaskService) AsyncAuditLogArchive(taskType string, taskData *task.AuditLogArchiveTaskData) error {
	s.log.Infof(context.Background(), "AsyncAuditLogArchive [%s] [%+v]", taskType, taskData)

	if s.auditLogArchiveRepo == nil {
		return errors.New("audit log archive repo is not configured")
	}

	outDir := os.Getenv("AUDIT_ARCHIVE_DIR")
	if outDir == "" {
		outDir = "./data/audit-archive"
	}
	retentionDays := 180
	if v := os.Getenv("AUDIT_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			retentionDays = n
		}
	}
	before := time.Now().AddDate(0, 0, -retentionDays)

	ctx := viewer.WithSystemContext(context.Background())
	archive := func() error {
		results, err := s.auditLogArchiveRepo.ArchiveExpired(ctx, before, outDir)
		if err != nil {
			s.log.Errorf(ctx, "audit log archive failed: %s", err.Error())
			return err
		}
		for table, n := range results {
			s.log.Infof(ctx, "audit log archived: %s -> %d rows (dir=%s)", table, n, outDir)
		}
		return nil
	}

	if s.rdb == nil {
		return archive()
	}
	// 多副本兜底：入队端的 asynq.Unique 在首个任务执行完后就释放，时钟偏差大的副本仍可能再入队一次。
	// 同一时间窗内只让一个实例真正归档，否则两边会把同一批行各导出一份。
	ran, err := redissync.RunOncePerWindow(ctx, s.rdb, auditLogArchiveOnceKey, task.AuditLogArchiveOnceWindow, archive)
	if err != nil {
		return err
	}
	if !ran {
		s.log.Infof(ctx, "audit log archive skipped: already run by another instance within %s", task.AuditLogArchiveOnceWindow)
	}
	return nil
}

// auditLogArchiveOnceKey 审计归档执行去重标记的 Redis 键。
var auditLogArchiveOnceKey = serviceid.ProjectName + ":cron-once:" + task.AuditLogArchiveTaskType
