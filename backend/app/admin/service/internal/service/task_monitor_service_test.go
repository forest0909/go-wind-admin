package service

import (
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/timeutil"

	taskV1 "go-wind-admin/api/gen/go/task/service/v1"
)

func TestScheduleEntryToDTO(t *testing.T) {
	next := time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	prev := time.Date(2026, 10, 3, 11, 0, 0, 0, time.Local)
	entry := &asynq.SchedulerEntry{
		ID:   "tenant_expiry_scan",
		Spec: "0 * * * *",
		Task: asynq.NewTask("tenant_expiry_scan", nil),
		Next: next,
		Prev: prev,
	}

	dto := scheduleEntryToDTO(entry)
	require.Equal(t, "tenant_expiry_scan", dto.GetTaskType())
	require.Equal(t, "0 * * * *", dto.GetCronSpec())
	require.Equal(t, next.Unix(), dto.GetNextEnqueueAt().AsTime().Unix())
	require.Equal(t, prev.Unix(), dto.GetPrevEnqueueAt().AsTime().Unix())
}

func TestScheduleEntryToDTONeverEnqueued(t *testing.T) {
	entry := &asynq.SchedulerEntry{
		Spec: "*/5 * * * *",
		Task: asynq.NewTask("notification_delivery_sweep", nil),
		// Next/Prev 零值：调度已注册但还没到第一个触发点
	}

	dto := scheduleEntryToDTO(entry)
	require.Nil(t, dto.GetPrevEnqueueAt(), "从未入队时 Prev 应保持空（前端按\"从未运行\"渲染）")
	require.Nil(t, dto.GetNextEnqueueAt(), "零值 Next 同样保持空")
}

func TestFailureToDTO(t *testing.T) {
	failedAt := time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)
	next := time.Date(2026, 10, 3, 10, 5, 0, 0, time.Local)
	ti := &asynq.TaskInfo{
		Type:          "notification_dispatch",
		Queue:         "default",
		LastErr:       "dial tcp: connection refused",
		LastFailedAt:  failedAt,
		Retried:       2,
		MaxRetry:      3,
		NextProcessAt: next,
	}

	dto := failureToDTO(ti, "retry")
	require.Equal(t, "notification_dispatch", dto.GetTaskType())
	require.Equal(t, "default", dto.GetQueue())
	require.Equal(t, "retry", dto.GetState())
	require.Equal(t, "dial tcp: connection refused", dto.GetLastError())
	require.Equal(t, uint32(2), dto.GetRetried())
	require.Equal(t, uint32(3), dto.GetMaxRetry())
	require.Equal(t, next.Unix(), dto.GetNextProcessAt().AsTime().Unix())
}

func TestHighestWeightQueue(t *testing.T) {
	require.Equal(t, "critical", highestWeightQueue(map[string]int32{"critical": 10, "default": 5, "low": 1}))
	require.Equal(t, "default", highestWeightQueue(map[string]int32{"default": 5}), "单队列直接取")
	require.Equal(t, "default", highestWeightQueue(map[string]int32{}), "空配置兜底 default")
}

func TestSortSummariesAndFailures(t *testing.T) {
	older := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	newer := time.Date(2026, 10, 3, 0, 0, 0, 0, time.Local)

	summaries := []*taskV1.SystemTaskStateSummary{
		{TaskType: "zzz"},
		{TaskType: "aaa"},
	}
	sortSummaries(summaries)
	require.Equal(t, "aaa", summaries[0].GetTaskType())

	failures := []*taskV1.SystemTaskFailure{
		{LastFailedAt: timeutil.TimeToTimestamppb(&older)},
		{LastFailedAt: timeutil.TimeToTimestamppb(&newer)},
		{LastFailedAt: nil},
	}
	sortFailures(failures)
	require.Equal(t, newer.Unix(), failures[0].GetLastFailedAt().AsTime().Unix())
	require.Equal(t, older.Unix(), failures[1].GetLastFailedAt().AsTime().Unix())
	require.Nil(t, failures[2].GetLastFailedAt(), "无失败时间的条目排最后")
}
