package data

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/protobuf/field_mask"

	"go-wind-admin/third_party/go-utils/trans"

	configV1 "go-wind-admin/api/gen/go/config/service/v1"
	entCrud "go-wind-admin/third_party/go-crud/entgo"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// newConfigRepoInstance 同一个库、同一个 Redis 上的一个"实例"。
func newConfigRepoInstance(t *testing.T, entClient *entCrud.EntClient[*ent.Client], addr string) *ConfigRepo {
	t.Helper()
	repo := newConfigRepoSqlite(t)
	repo.entClient = entClient
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	repo.rdb = rdb
	return repo
}

func setMinLen(t *testing.T, repo *ConfigRepo, ctx context.Context, v string) {
	t.Helper()
	require.NoError(t, repo.Update(ctx, &configV1.UpdateConfigRequest{
		Id:         1,
		Data:       &configV1.Config{Value: trans.Ptr(v)},
		UpdateMask: &field_mask.FieldMask{Paths: []string{"value"}},
	}))
}

// a4 F10：B 的订阅断开期间 A 改了参数，失效广播丢失。修复前 B 重连后永远读旧值；
// 现在订阅重建时清空本地缓存。
func TestConfigRepo_ResubscribeClearsCache(t *testing.T) {
	mr := miniredis.RunT(t)
	entClient := enttest.NewEntClientForTest(t)
	ctx := newConfigRepoCtx()

	a := newConfigRepoInstance(t, entClient, mr.Addr())
	b := newConfigRepoInstance(t, entClient, mr.Addr())
	require.NoError(t, a.Create(ctx, &configV1.CreateConfigRequest{Data: &configV1.Config{
		Key: trans.Ptr("sys.password.minLen"), Value: trans.Ptr("8"), ValueType: configV1.Config_INT.Enum(),
	}}))

	// 在线时广播生效
	bctx, cancelB := context.WithCancel(context.Background())
	go b.subscribeInvalidations(bctx)
	require.Eventually(t, func() bool { return mr.PubSubNumSub(configInvalidateChannel)[configInvalidateChannel] == 1 },
		5*time.Second, 10*time.Millisecond)
	require.Equal(t, 8, b.GetConfigInt(ctx, "sys.password.minLen", 0))
	setMinLen(t, a, ctx, "10")
	require.Eventually(t, func() bool { return b.GetConfigInt(ctx, "sys.password.minLen", 0) == 10 },
		5*time.Second, 10*time.Millisecond)

	// B 掉线期间 A 改成 16：广播丢失，B 仍是旧缓存
	cancelB()
	require.Eventually(t, func() bool { return mr.PubSubNumSub(configInvalidateChannel)[configInvalidateChannel] == 0 },
		5*time.Second, 10*time.Millisecond)
	setMinLen(t, a, ctx, "16")
	require.Equal(t, 10, b.GetConfigInt(ctx, "sys.password.minLen", 0), "前提：广播丢失时 B 仍命中旧缓存")

	// B 重新订阅 → 清空缓存 → 读到新值
	bctx2, cancelB2 := context.WithCancel(context.Background())
	defer cancelB2()
	go b.subscribeInvalidations(bctx2)
	require.Eventually(t, func() bool { return b.GetConfigInt(ctx, "sys.password.minLen", 0) == 16 },
		5*time.Second, 10*time.Millisecond)
}

// 缓存条目有上限寿命：没有任何广播（例如绕过 repo 直改表）时，过了 TTL 也会重新查库。
func TestConfigRepo_CacheEntryExpires(t *testing.T) {
	repo := newConfigRepoSqlite(t)
	ctx := newConfigRepoCtx()
	now := time.Now()
	configNow = func() time.Time { return now }
	t.Cleanup(func() { configNow = time.Now })

	require.NoError(t, repo.Create(ctx, &configV1.CreateConfigRequest{Data: &configV1.Config{
		Key: trans.Ptr("sys.ttl.probe"), Value: trans.Ptr("1"), ValueType: configV1.Config_INT.Enum(),
	}}))
	require.Equal(t, 1, repo.GetConfigInt(ctx, "sys.ttl.probe", 0))

	// 直改表，不走 repo，不会有失效
	_, err := repo.entClient.Client().SysConfig.Update().SetValue("2").Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, repo.GetConfigInt(ctx, "sys.ttl.probe", 0), "TTL 内命中缓存")

	now = now.Add(sysConfigCacheTTL + time.Second)
	require.Equal(t, 2, repo.GetConfigInt(ctx, "sys.ttl.probe", 0), "过了 TTL 应重新查库")
}
