// TaskService 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标：
//   - 调度器未配置守卫：ListTaskTypeName 返回空、NewTask 报错、
//     StopAllTask 不 panic、startTask/stopTask 报"未配置"、
//     AsyncAuditLogArchive 的依赖未配置报错。
//   - Create：nil Data / 未注册 typeName / 调度器未配置拒绝；注册 typeName 下
//     禁用任务只落库不进调度器；启用任务按类型进 PERIODIC / DELAY /
//     WAIT_RESULT 调度入口；操作人盖章。
//   - Update：启用开关单字段提交时 typeName 从旧行回填；未注册回填名拒绝；
//     启用/停用分别触发注册/注销。
//   - Delete：PERIODIC 任务删除时同步注销调度项。
//   - ControlTask：Stop/Start/Restart 的调度调用（租户上下文按 typeName 取行）、
//     未知控制类型拒绝、平台上下文按名查询的租户闸门、停用任务与一次性任务的
//     停止语义拒绝。
//   - StartAllTask/RestartAllTask：调度项按任务行 ID 绑定（同名互不覆盖）、DELAY 一次性投递、
//     系统级常驻审计归档只注册一次；StopAllTask 只注销用户任务；系统任务类型保留。
//   - 纯函数：convertTaskOption（载荷 JSON 解析/空载荷兜底/选项转换）。
//
// 跳过项：真实 asynq 调度器与 Redis 队列（调度器全部走本地桩）、
// AsyncAuditLogArchive 全链路
// （auditLogArchiveRepo 无 testkit 构造器，仅测未配置守卫）。
package service

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go-wind-admin/third_party/go-utils/trans"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	paginationV1 "go-wind-admin/third_party/go-crud/api/gen/go/pagination/v1"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	taskV1 "go-wind-admin/api/gen/go/task/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/task"
)

// taskSchedulerStub 是 TaskScheduler 接口的本地测试桩：记录全部调度调用。
// live 是当前仍注册着的周期调度项（entryID → typeName）。
type taskSchedulerStub struct {
	registered        map[string]bool
	newTask           []string
	newWaitResultTask []string
	newPeriodicTask   []string
	removedPeriodic   []string
	live              map[string]string
	nextEntry         int
}

func newTaskSchedulerStub(types ...string) *taskSchedulerStub {
	s := &taskSchedulerStub{registered: map[string]bool{}, live: map[string]string{}}
	for _, t := range types {
		s.registered[t] = true
	}
	return s
}

func (s *taskSchedulerStub) TaskTypeExists(taskType string) bool { return s.registered[taskType] }
func (s *taskSchedulerStub) GetRegisteredTaskTypes() []string {
	out := make([]string, 0, len(s.registered))
	for k := range s.registered {
		out = append(out, k)
	}
	return out
}
func (s *taskSchedulerStub) NewTask(typeName string, _ any, _ ...asynq.Option) error {
	s.newTask = append(s.newTask, typeName)
	return nil
}
func (s *taskSchedulerStub) NewWaitResultTask(typeName string, _ any, _ ...asynq.Option) error {
	s.newWaitResultTask = append(s.newWaitResultTask, typeName)
	return nil
}
func (s *taskSchedulerStub) NewPeriodicTask(_ string, typeName string, _ any, _ ...asynq.Option) (string, error) {
	s.newPeriodicTask = append(s.newPeriodicTask, typeName)
	s.nextEntry++
	entryID := fmt.Sprintf("entry-%d", s.nextEntry)
	s.live[entryID] = typeName
	return entryID, nil
}
func (s *taskSchedulerStub) RemovePeriodicTaskByID(entryID string) error {
	typeName, ok := s.live[entryID]
	if !ok {
		return fmt.Errorf("entry %s not exist", entryID)
	}
	delete(s.live, entryID)
	s.removedPeriodic = append(s.removedPeriodic, typeName)
	return nil
}

// liveTypeNames 当前仍注册的周期调度项的 typeName（可重复，已排序）。
func (s *taskSchedulerStub) liveTypeNames() []string {
	out := make([]string, 0, len(s.live))
	for _, v := range s.live {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// newTaskServiceForTest 白盒构造 TaskService（log 换 NopLogger helper，
// taskRepo 用 testkit 构造器）；其余依赖按用例置空/注入。
func newTaskServiceForTest(t *testing.T, scheduler TaskScheduler) (*TaskService, *ent.Client) {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	svc := &TaskService{
		log:                 bLogger.NewHelper(bLogger.NopLogger()),
		taskRepo:            data.NewTaskRepoForTest(entClient),
		userRepo:            nil,
		auditLogArchiveRepo: nil,
	}
	if scheduler != nil {
		svc.RegisterTaskScheduler(scheduler)
	}
	return svc, entClient.Client()
}

// seedTaskRow 经 repo 直落一条任务行（平台上下文 + 显式租户，绕过服务层，
// 便于单独断言调度行为）。
func seedTaskRow(t *testing.T, svc *TaskService, ctx context.Context, tenantID uint32, typeName string, typ taskV1.Task_Type, cron string, enable bool) {
	t.Helper()
	_, err := svc.taskRepo.Create(ctx, &taskV1.CreateTaskRequest{
		Data: &taskV1.Task{
			TenantId: trans.Ptr(tenantID),
			Type:     trans.Ptr(typ),
			TypeName: trans.Ptr(typeName),
			CronSpec: trans.Ptr(cron),
			Enable:   trans.Ptr(enable),
			Remark:   trans.Ptr("tasksvc 测试行"),
		},
	})
	require.NoError(t, err, "落任务行 %s 应成功", typeName)
}

// ── 调度器未配置守卫 ───────────────────────────────────────────────────────

// TestTaskService_NoSchedulerGuards 验证调度器未配置时的各守卫分支。
func TestTaskService_NoSchedulerGuards(t *testing.T) {
	svc, _ := newTaskServiceForTest(t, nil)
	ctx := enttest.NewSystemContext(context.Background())

	resp, err := svc.ListTaskTypeName(ctx, &emptypb.Empty{})
	require.NoError(t, err, "未配置调度器时 ListTaskTypeName 应返回空列表而非 panic")
	require.Empty(t, resp.GetTypeNames())

	require.Error(t, svc.NewTask("any", nil), "未配置调度器时 NewTask 应报错")
	require.NotPanics(t, func() { svc.StopAllTask(ctx, &emptypb.Empty{}) },
		"未配置调度器时 StopAllTask 应 no-op 不 panic")

	require.EqualError(t, svc.startTask(nil), "task is nil")
	require.EqualError(t, svc.stopTask(nil), "task is nil")
	require.EqualError(t, svc.startTask(&taskV1.Task{Enable: trans.Ptr(false)}), "task is not enable")
	require.EqualError(t, svc.stopTask(&taskV1.Task{Enable: trans.Ptr(false)}), "task is not enable")
	require.EqualError(t, svc.startTask(&taskV1.Task{Enable: trans.Ptr(true)}), "task scheduler is not configured")
	require.EqualError(t, svc.stopTask(&taskV1.Task{Enable: trans.Ptr(true)}), "task scheduler is not configured")

	require.EqualError(t, svc.AsyncAuditLogArchive(task.AuditLogArchiveTaskType, &task.AuditLogArchiveTaskData{}),
		"audit log archive repo is not configured")
}

// TestTaskService_StopOneShotSemantics 调度器在位时的一次性任务停止语义：
// DELAY/WAIT_RESULT 无法安全停止（排队中/在途），必须诚实报错。
func TestTaskService_StopOneShotSemantics(t *testing.T) {
	svc, _ := newTaskServiceForTest(t, newTaskSchedulerStub())

	require.EqualError(t, svc.stopTask(&taskV1.Task{
		Type: taskV1.Task_DELAY.Enum(), TypeName: trans.Ptr("x"), Enable: trans.Ptr(true),
	}), "queued one-shot task cannot be stopped, it will run at its scheduled time")
	require.EqualError(t, svc.stopTask(&taskV1.Task{
		Type: taskV1.Task_WAIT_RESULT.Enum(), TypeName: trans.Ptr("x"), Enable: trans.Ptr(true),
	}), "in-flight task cannot be stopped, please wait for it to finish")
}

// ── Create ────────────────────────────────────────────────────────────────

// TestTaskService_CreateValidation 验证 Create 的校验分支：
// nil Data 拒绝、未注册 typeName 拒绝（含未配置调度器）。
func TestTaskService_CreateValidation(t *testing.T) {
	svc, _ := newTaskServiceForTest(t, newTaskSchedulerStub("tasksvc_reg_type"))
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &taskV1.CreateTaskRequest{})
	require.Error(t, err, "nil Data 应拒绝")
	require.True(t, adminV1.IsBadRequest(err))

	_, err = svc.Create(opCtx, &taskV1.CreateTaskRequest{
		Data: &taskV1.Task{
			Type:     taskV1.Task_PERIODIC.Enum(),
			TypeName: trans.Ptr("tasksvc_unreg_type"),
			Enable:   trans.Ptr(true),
		},
	})
	require.Error(t, err, "未注册 typeName 应拒绝")
	require.True(t, adminV1.IsBadRequest(err))

	svc.taskScheduler = nil
	_, err = svc.Create(opCtx, &taskV1.CreateTaskRequest{
		Data: &taskV1.Task{
			Type:     taskV1.Task_PERIODIC.Enum(),
			TypeName: trans.Ptr("tasksvc_reg_type"),
			Enable:   trans.Ptr(true),
		},
	})
	require.Error(t, err, "调度器未配置时注册 typeName 也应拒绝")
	require.True(t, adminV1.IsBadRequest(err))
}

// TestTaskService_CreateDisabledSkipsScheduler 验证禁用任务只落库不进调度器。
func TestTaskService_CreateDisabledSkipsScheduler(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_reg_type")
	svc, client := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.Create(opCtx, &taskV1.CreateTaskRequest{
		Data: &taskV1.Task{
			Type:     taskV1.Task_PERIODIC.Enum(),
			TypeName: trans.Ptr("tasksvc_reg_type"),
			CronSpec: trans.Ptr("0 */5 * * * *"),
			Enable:   trans.Ptr(false),
		},
	})
	require.NoError(t, err, "禁用任务创建（落库）应成功")
	require.Empty(t, stub.newPeriodicTask, "禁用任务不应注册周期调度项")
	require.Empty(t, stub.newTask, "禁用任务不应投递一次性任务")
	rows, err := client.Task.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "禁用任务应落库")
	require.Equal(t, uint32(7), *rows[0].CreatedBy, "创建行应盖入操作人 ID")
}

// TestTaskService_CreateEnabledEntersScheduler 验证启用任务按类型进调度入口：
// PERIODIC→NewPeriodicTask、DELAY→NewTask、WAIT_RESULT→NewWaitResultTask。
func TestTaskService_CreateEnabledEntersScheduler(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_periodic_type", "tasksvc_delay_type", "tasksvc_wait_type")
	svc, _ := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	for _, tc := range []struct {
		typ  taskV1.Task_Type
		name string
	}{
		{taskV1.Task_PERIODIC, "tasksvc_periodic_type"},
		{taskV1.Task_DELAY, "tasksvc_delay_type"},
		{taskV1.Task_WAIT_RESULT, "tasksvc_wait_type"},
	} {
		_, err := svc.Create(opCtx, &taskV1.CreateTaskRequest{
			Data: &taskV1.Task{
				Type:     trans.Ptr(tc.typ),
				TypeName: trans.Ptr(tc.name),
				CronSpec: trans.Ptr("0 */5 * * * *"),
				Enable:   trans.Ptr(true),
			},
		})
		require.NoError(t, err, "注册类型 %s 的启用任务创建应成功", tc.name)
	}
	require.Equal(t, []string{"tasksvc_periodic_type"}, stub.newPeriodicTask,
		"启用 PERIODIC 任务应注册周期调度项")
	require.Equal(t, []string{"tasksvc_delay_type"}, stub.newTask,
		"启用 DELAY 任务应投递一次性任务")
	require.Equal(t, []string{"tasksvc_wait_type"}, stub.newWaitResultTask,
		"启用 WAIT_RESULT 任务应投递等待结果任务")
}

// ── Update / Delete ───────────────────────────────────────────────────────

// TestTaskService_UpdateEnableToggleBackfillsTypeName 验证启用开关单字段提交：
// typeName 从旧行回填；启用触发先注销旧项后注册新项、停用只注销。
func TestTaskService_UpdateEnableToggleBackfillsTypeName(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_reg_type")
	svc, _ := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	seedTaskRow(t, svc, ctx, 0, "tasksvc_reg_type", taskV1.Task_PERIODIC, "0 */5 * * * *", false)
	rows, err := svc.taskRepo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	require.NoError(t, err)
	require.Len(t, rows.GetItems(), 1)
	rowID := rows.GetItems()[0].GetId()

	// 启用：回填 typeName → 注销旧调度项（无）→ 注册新调度项
	_, err = svc.Update(opCtx, &taskV1.UpdateTaskRequest{
		Id:   rowID,
		Data: &taskV1.Task{Enable: trans.Ptr(true)},
	})
	require.NoError(t, err, "启用开关更新应成功（typeName 回填自旧行）")
	require.Equal(t, []string{"tasksvc_reg_type"}, stub.newPeriodicTask,
		"启用应经回填的 typeName 注册周期调度项")

	// 停用：不注册新项，但仍先注销旧项（生产语义：启用与停用更新都先注销，
	// 以清除可能残留的调度项）
	stub.newPeriodicTask = nil
	_, err = svc.Update(opCtx, &taskV1.UpdateTaskRequest{
		Id:   rowID,
		Data: &taskV1.Task{Enable: trans.Ptr(false)},
	})
	require.NoError(t, err, "停用开关更新应成功")
	require.Empty(t, stub.newPeriodicTask, "停用不应注册新调度项")
	require.Equal(t, []string{"tasksvc_reg_type"}, stub.removedPeriodic,
		"停用应注销启用时注册的调度项（绕过 enable 保护）")
	require.Empty(t, stub.live, "停用后不应残留调度项")
}

// TestTaskService_UpdateUnregisteredBackfillRejected 验证回填出的旧 typeName
// 未注册时更新被拒。
func TestTaskService_UpdateUnregisteredBackfillRejected(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_other_type")
	svc, _ := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	seedTaskRow(t, svc, ctx, 0, "tasksvc_unreg_backfill", taskV1.Task_PERIODIC, "0 */5 * * * *", false)
	rows, err := svc.taskRepo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	require.NoError(t, err)
	rowID := rows.GetItems()[0].GetId()

	_, err = svc.Update(opCtx, &taskV1.UpdateTaskRequest{
		Id:   rowID,
		Data: &taskV1.Task{Enable: trans.Ptr(true)},
	})
	require.Error(t, err, "旧行 typeName 未注册时更新应拒绝")
	require.True(t, adminV1.IsBadRequest(err))
	require.Empty(t, stub.newPeriodicTask, "拒绝路径不应注册调度项")
}

// TestTaskService_UpdateDeleteMissingId 验证不存在的任务 ID 更新/删除报错。
func TestTaskService_UpdateDeleteMissingId(t *testing.T) {
	svc, _ := newTaskServiceForTest(t, newTaskSchedulerStub("tasksvc_reg_type"))
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	_, err := svc.Update(opCtx, &taskV1.UpdateTaskRequest{
		Id:   9999999,
		Data: &taskV1.Task{Enable: trans.Ptr(true)},
	})
	require.Error(t, err, "不存在的任务 ID 更新应报错")

	_, err = svc.Delete(ctx, &taskV1.DeleteTaskRequest{
		QueryBy: &taskV1.DeleteTaskRequest_Id{Id: 88888888},
	})
	require.Error(t, err, "删除不存在的任务应报错")
}

// TestTaskService_DeleteRemovesSchedulerEntry 验证 PERIODIC 任务删除时
// 同步注销调度项、删除后清库。
func TestTaskService_DeleteRemovesSchedulerEntry(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_del_type")
	svc, client := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())

	seedTaskRow(t, svc, ctx, 0, "tasksvc_del_type", taskV1.Task_PERIODIC, "0 */5 * * * *", true)
	rows, err := svc.taskRepo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	require.NoError(t, err)
	rowID := rows.GetItems()[0].GetId()
	_, err = svc.StartAllTask(ctx, &emptypb.Empty{})
	require.NoError(t, err)

	_, err = svc.Delete(ctx, &taskV1.DeleteTaskRequest{
		QueryBy: &taskV1.DeleteTaskRequest_Id{Id: rowID},
	})
	require.NoError(t, err, "删除已存在任务应成功")
	require.Equal(t, []string{"tasksvc_del_type"}, stub.removedPeriodic,
		"删除 PERIODIC 任务应注销其调度项（绕过 enable 保护）")
	require.Equal(t, []string{task.AuditLogArchiveTaskType}, stub.liveTypeNames(),
		"删除用户任务不应影响系统调度项")
	cnt, err := client.Task.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, cnt, "删除后任务表应清空")
}

// ── ControlTask ───────────────────────────────────────────────────────────

// TestTaskService_ControlTaskGuards 验证 ControlTask 的守卫分支：
// 未知控制类型拒绝、按不存在的名查询报错、
// 停用任务停止拒绝、一次性任务停止语义拒绝。
func TestTaskService_ControlTaskGuards(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_ctl_type", "tasksvc_delay_ctl")
	svc, _ := newTaskServiceForTest(t, stub)
	sysCtx := enttest.NewSystemContext(context.Background())
	opCtx := sysCtx

	// 未知控制类型：取到任务行后按控制类型分派，未知类型 400
	seedTaskRow(t, svc, sysCtx, 0, "tasksvc_ctl_type", taskV1.Task_PERIODIC, "0 */5 * * * *", true)
	_, err := svc.ControlTask(sysCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_type",
		ControlType: taskV1.ControlTaskRequest_ControlType(99),
	})
	require.Error(t, err, "未知控制类型应拒绝")
	require.True(t, adminV1.IsBadRequest(err))

	// 按不存在的名查询：报错
	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_no_such_type",
		ControlType: taskV1.ControlTaskRequest_Stop,
	})
	require.Error(t, err, "查询不存在的任务名应报错")

	// 停用任务：stopTask 拒绝
	seedTaskRow(t, svc, sysCtx, 0, "tasksvc_disabled_ctl", taskV1.Task_PERIODIC, "0 */5 * * * *", false)
	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_disabled_ctl",
		ControlType: taskV1.ControlTaskRequest_Stop,
	})
	require.EqualError(t, err, "task is not enable", "停用任务的停止应拒绝")

	// 一次性任务（DELAY）停止语义拒绝
	seedTaskRow(t, svc, sysCtx, 0, "tasksvc_delay_ctl", taskV1.Task_DELAY, "0 */5 * * * *", true)
	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_delay_ctl",
		ControlType: taskV1.ControlTaskRequest_Stop,
	})
	require.EqualError(t, err, "queued one-shot task cannot be stopped, it will run at its scheduled time",
		"DELAY 任务停止应被语义拒绝")
}

// TestTaskService_ControlTaskDispatch 启用 PERIODIC 任务的
// Stop/Start/Restart 调度分发。
func TestTaskService_ControlTaskDispatch(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_ctl_dispatch")
	svc, _ := newTaskServiceForTest(t, stub)
	sysCtx := enttest.NewSystemContext(context.Background())
	opCtx := sysCtx

	seedTaskRow(t, svc, sysCtx, 0, "tasksvc_ctl_dispatch", taskV1.Task_PERIODIC, "0 */5 * * * *", true)

	_, err := svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_dispatch",
		ControlType: taskV1.ControlTaskRequest_Stop,
	})
	require.EqualError(t, err, "periodic task is not scheduled", "未调度的任务 Stop 应报错")

	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_dispatch",
		ControlType: taskV1.ControlTaskRequest_Start,
	})
	require.NoError(t, err, "Start 应成功")
	require.Equal(t, []string{"tasksvc_ctl_dispatch"}, stub.newPeriodicTask, "Start 应注册调度项")

	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_dispatch",
		ControlType: taskV1.ControlTaskRequest_Stop,
	})
	require.NoError(t, err, "Stop 应成功")
	require.Equal(t, []string{"tasksvc_ctl_dispatch"}, stub.removedPeriodic, "Stop 应注销调度项")
	require.Empty(t, stub.live)

	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_dispatch",
		ControlType: taskV1.ControlTaskRequest_Start,
	})
	require.NoError(t, err)

	stub.removedPeriodic = nil
	stub.newPeriodicTask = nil
	_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{
		TypeName:    "tasksvc_ctl_dispatch",
		ControlType: taskV1.ControlTaskRequest_Restart,
	})
	require.NoError(t, err, "Restart 应成功（桩两次调用均返回 nil）")
	require.Equal(t, []string{"tasksvc_ctl_dispatch"}, stub.removedPeriodic, "Restart 应先注销")
	require.Equal(t, []string{"tasksvc_ctl_dispatch"}, stub.newPeriodicTask, "Restart 应后注册")
}

// ── StartAll / StopAll / RestartAll ────────────────────────────────────────

// TestTaskService_RestartAllTask 验证 RestartAllTask：只替换用户任务的调度项，
// 同名 PERIODIC（遗留的多租户行）各自注册、互不覆盖，DELAY 一次性投递，
// 系统级审计归档始终只有一个调度项。
func TestTaskService_RestartAllTask(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_dup_type", "tasksvc_delay_all_type")
	svc, _ := newTaskServiceForTest(t, stub)
	sysCtx := enttest.NewSystemContext(context.Background())

	// 两条同名 PERIODIC（分属不同租户规避唯一约束）+ 一条 DELAY
	seedTaskRow(t, svc, sysCtx, 42, "tasksvc_dup_type", taskV1.Task_PERIODIC, "0 */5 * * * *", true)
	seedTaskRow(t, svc, sysCtx, 43, "tasksvc_dup_type", taskV1.Task_PERIODIC, "0 */5 * * * *", true)
	seedTaskRow(t, svc, sysCtx, 0, "tasksvc_delay_all_type", taskV1.Task_DELAY, "0 */5 * * * *", true)

	_, err := svc.StartAllTask(sysCtx, &emptypb.Empty{})
	require.NoError(t, err)
	want := []string{task.AuditLogArchiveTaskType, "tasksvc_dup_type", "tasksvc_dup_type"}
	require.Equal(t, want, stub.liveTypeNames())

	resp, err := svc.RestartAllTask(sysCtx, &emptypb.Empty{})
	require.NoError(t, err, "RestartAllTask 应成功")
	require.Equal(t, int32(3), resp.GetCount(),
		"成功计数应为 3（两条 PERIODIC + DELAY；系统级常驻注册不计入返回值）")
	require.Equal(t, []string{"tasksvc_dup_type", "tasksvc_dup_type"}, stub.removedPeriodic,
		"重启只注销用户任务的调度项，系统调度项不动")
	require.Equal(t, want, stub.liveTypeNames(), "重启后调度项集合不变，系统调度项不重复注册")
	require.Equal(t, []string{"tasksvc_delay_all_type", "tasksvc_delay_all_type"}, stub.newTask,
		"DELAY 任务每次启动都经一次性投递入口")
}

// TestTaskService_StopAllTask 验证 StopAllTask 只注销用户任务，系统调度项保留。
// 修复前它调用调度器的 RemoveAllPeriodicTask，审计归档的 cron 也被一起注销。
func TestTaskService_StopAllTask(t *testing.T) {
	stub := newTaskSchedulerStub("tasksvc_stop_type")
	svc, _ := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())

	seedTaskRow(t, svc, ctx, 0, "tasksvc_stop_type", taskV1.Task_PERIODIC, "0 */5 * * * *", true)
	_, err := svc.StartAllTask(ctx, &emptypb.Empty{})
	require.NoError(t, err)

	_, err = svc.StopAllTask(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, []string{"tasksvc_stop_type"}, stub.removedPeriodic)
	require.Equal(t, []string{task.AuditLogArchiveTaskType}, stub.liveTypeNames(),
		"StopAllTask 后系统级审计归档调度项必须还在")

	// 再次 StartAll 不应重复注册系统调度项
	_, err = svc.StartAllTask(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, []string{task.AuditLogArchiveTaskType, "tasksvc_stop_type"}, stub.liveTypeNames())
}

// TestTaskService_SystemTaskTypesReserved 系统任务类型不能经任务管理接口创建、修改、启停，
// 也不出现在类型列表里；遗留的系统类型行不会被调度，删除它也不会动系统调度项。
// 修复前 Create(typeName=broadcast_message) 会把用户提交的 payload 原样入队给广播 handler。
func TestTaskService_SystemTaskTypesReserved(t *testing.T) {
	stub := newTaskSchedulerStub(task.BroadcastMessageTaskType, task.AuditLogArchiveTaskType, "tasksvc_user_type")
	svc, client := newTaskServiceForTest(t, stub)
	ctx := enttest.NewSystemContext(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	resp, err := svc.ListTaskTypeName(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, []string{"tasksvc_user_type"}, resp.GetTypeNames(), "系统类型不应列出")

	for _, name := range []string{task.BroadcastMessageTaskType, task.AuditLogArchiveTaskType} {
		_, err = svc.Create(opCtx, &taskV1.CreateTaskRequest{Data: &taskV1.Task{
			Type:        taskV1.Task_DELAY.Enum(),
			TypeName:    trans.Ptr(name),
			TaskPayload: trans.Ptr(`{"message_id":1}`),
			Enable:      trans.Ptr(true),
		}})
		require.True(t, adminV1.IsForbidden(err), "创建系统类型 %s 应 403，实际 %v", name, err)
	}
	require.Empty(t, stub.newTask, "系统类型任务不能入队")
	n, err := client.Task.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "拒绝时不落库")

	// 把用户任务改成系统类型
	seedTaskRow(t, svc, ctx, 0, "tasksvc_user_type", taskV1.Task_PERIODIC, "0 */5 * * * *", false)
	userRow, err := client.Task.Query().Only(ctx)
	require.NoError(t, err)
	_, err = svc.Update(opCtx, &taskV1.UpdateTaskRequest{Id: userRow.ID, Data: &taskV1.Task{
		TypeName: trans.Ptr(task.BroadcastMessageTaskType), Enable: trans.Ptr(true),
	}})
	require.True(t, adminV1.IsForbidden(err), "改成系统类型应 403，实际 %v", err)

	// 旧版本留下的系统类型行：启动时跳过，不能 Update / Control
	seedTaskRow(t, svc, ctx, 0, task.AuditLogArchiveTaskType, taskV1.Task_PERIODIC, "* * * * *", true)
	legacy, err := svc.taskRepo.Get(ctx, &taskV1.GetTaskRequest{QueryBy: &taskV1.GetTaskRequest_TypeName{TypeName: task.AuditLogArchiveTaskType}})
	require.NoError(t, err)

	_, err = svc.StartAllTask(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, []string{task.AuditLogArchiveTaskType}, stub.liveTypeNames(),
		"只有系统自己注册的归档调度项，遗留行不调度")

	_, err = svc.Update(opCtx, &taskV1.UpdateTaskRequest{Id: legacy.GetId(), Data: &taskV1.Task{Enable: trans.Ptr(false)}})
	require.True(t, adminV1.IsForbidden(err), "修改遗留系统类型行应 403，实际 %v", err)
	for _, ct := range []taskV1.ControlTaskRequest_ControlType{
		taskV1.ControlTaskRequest_Stop, taskV1.ControlTaskRequest_Start, taskV1.ControlTaskRequest_Restart,
	} {
		_, err = svc.ControlTask(opCtx, &taskV1.ControlTaskRequest{TypeName: task.AuditLogArchiveTaskType, ControlType: ct})
		require.True(t, adminV1.IsForbidden(err), "控制系统类型 %v 应 403，实际 %v", ct, err)
	}
	require.Equal(t, []string{task.AuditLogArchiveTaskType}, stub.liveTypeNames())

	// 删除遗留行只清理这一行，系统调度项不受影响（修复前按 typeName 注销会把它删掉）
	_, err = svc.Delete(opCtx, &taskV1.DeleteTaskRequest{QueryBy: &taskV1.DeleteTaskRequest_Id{Id: legacy.GetId()}})
	require.NoError(t, err)
	require.Equal(t, []string{task.AuditLogArchiveTaskType}, stub.liveTypeNames())
	require.Empty(t, stub.removedPeriodic)
}

// TestTaskService_AuditLogArchiveOncePerWindow 两个实例（两个 TaskService 共享同一个库和 Redis）
// 各自的 cron 都入队了同一轮归档，只有先到的那个真正归档；失败不占用时间窗。
func TestTaskService_AuditLogArchiveOncePerWindow(t *testing.T) {
	mr := miniredis.RunT(t)
	entClient := enttest.NewEntClientForTest(t)
	newInstance := func() *TaskService {
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = rdb.Close() })
		return &TaskService{
			log:                 bLogger.NewHelper(bLogger.NopLogger()),
			taskRepo:            data.NewTaskRepoForTest(entClient),
			auditLogArchiveRepo: data.NewAuditLogArchiveRepoForTest(entClient),
			rdb:                 rdb,
		}
	}
	a, b := newInstance(), newInstance()

	t.Setenv("AUDIT_ARCHIVE_DIR", t.TempDir())
	t.Setenv("AUDIT_RETENTION_DAYS", "30")
	ctx := enttest.NewSystemContext(context.Background())
	client := entClient.Client()
	seedOld := func(name string) {
		require.NoError(t, client.LoginAuditLog.Create().
			SetCreatedAt(time.Now().AddDate(0, 0, -60)).
			SetNillableUsername(trans.Ptr(name)).Exec(ctx))
	}
	countLogin := func() int {
		n, err := client.LoginAuditLog.Query().Count(ctx)
		require.NoError(t, err)
		return n
	}

	seedOld("first")
	require.NoError(t, a.AsyncAuditLogArchive(task.AuditLogArchiveTaskType, &task.AuditLogArchiveTaskData{}))
	require.Zero(t, countLogin(), "实例 A 应完成归档")

	// 同一时间窗内实例 B 收到的重复任务直接跳过：新插入的过期行不应被它归档
	seedOld("second")
	require.NoError(t, b.AsyncAuditLogArchive(task.AuditLogArchiveTaskType, &task.AuditLogArchiveTaskData{}))
	require.Equal(t, 1, countLogin(), "同一时间窗内的重复任务不应再次执行")

	// 时间窗过后（下一轮）可以再执行
	mr.FastForward(task.AuditLogArchiveOnceWindow + time.Second)
	require.NoError(t, b.AsyncAuditLogArchive(task.AuditLogArchiveTaskType, &task.AuditLogArchiveTaskData{}))
	require.Zero(t, countLogin())
}

// ── 纯函数 ────────────────────────────────────────────────────────────────

// TestTaskService_ConvertTaskOption 验证选项转换：载荷 JSON 解析、
// 空载荷兜底空对象、nil 任务返回空、任务选项逐项转换。
func TestTaskService_ConvertTaskOption(t *testing.T) {
	svc, _ := newTaskServiceForTest(t, nil)

	opts, payload := svc.convertTaskOption(nil)
	require.Nil(t, opts)
	require.Nil(t, payload)

	opts, payload = svc.convertTaskOption(&taskV1.Task{})
	require.Nil(t, opts)
	require.Equal(t, map[string]any{}, payload, "空载荷应兜底为空对象（asynq 拒绝 nil message）")

	opts, payload = svc.convertTaskOption(&taskV1.Task{
		TaskPayload: trans.Ptr(`{"alpha":1,"beta":"two"}`),
	})
	require.Nil(t, opts)
	require.Equal(t, map[string]any{"alpha": float64(1), "beta": "two"}, payload,
		"JSON 载荷应解析为参数对象")

	opts, _ = svc.convertTaskOption(&taskV1.Task{
		TaskPayload: trans.Ptr(`{}`),
		TaskOptions: &taskV1.TaskOption{
			MaxRetry:  trans.Ptr(uint32(2)),
			Timeout:   durationpb.New(5 * time.Second),
			Deadline:  timestamppb.New(time.Now().Add(time.Hour)),
			ProcessIn: durationpb.New(time.Second),
			ProcessAt: timestamppb.New(time.Now().Add(time.Minute)),
			UniqueTtl: durationpb.New(time.Minute),
			Retention: durationpb.New(time.Hour),
			Group:     trans.Ptr("grp"),
			TaskId:    trans.Ptr("tid-1"),
		},
	})
	require.Len(t, opts, 9, "全部 9 个任务选项都应转换为 asynq 选项")
}
