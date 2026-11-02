package task

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIsSystemTaskType(t *testing.T) {
	for _, name := range []string{BroadcastMessageTaskType, AuditLogArchiveTaskType} {
		if !IsSystemTaskType(name) {
			t.Errorf("%s should be a system task type", name)
		}
	}
	if IsSystemTaskType("user_defined") || IsSystemTaskType("") {
		t.Error("non-system names reported as system")
	}
}

// 广播 handler 只按 message_id 工作，payload 里不能有租户/范围字段可供信任；
// 升级前入队的旧任务带 tenant_id，解码时必须被丢弃。
func TestBroadcastMessageTaskData_IgnoresLegacyTenant(t *testing.T) {
	if n := reflect.TypeOf(BroadcastMessageTaskData{}).NumField(); n != 1 {
		t.Fatalf("BroadcastMessageTaskData must carry only message_id, has %d fields", n)
	}
	var d BroadcastMessageTaskData
	if err := json.Unmarshal([]byte(`{"message_id":5,"tenant_id":7}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.MessageId != 5 {
		t.Fatalf("message_id = %d, want 5", d.MessageId)
	}
}
