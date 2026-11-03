package data

import (
	"time"

	"github.com/redis/go-redis/v9"
	"go-wind-admin/third_party/go-utils/captcha"
	"go-wind-admin/third_party/go-utils/password"

	klog "github.com/go-kratos/kratos/v2/log"
	"go-wind-admin/third_party/kratos-bootstrap/bootstrap"
	redisClient "go-wind-admin/third_party/kratos-bootstrap/cache/redis"
	bLogger "go-wind-admin/third_party/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"

	"go-wind-admin/pkg/extdb"
	"go-wind-admin/pkg/oss"
	"go-wind-admin/pkg/serviceid"
	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

func NewClientType() authenticationV1.ClientType {
	return authenticationV1.ClientType_admin
}

// NewRedisClient 创建Redis客户端
func NewRedisClient(ctx *bootstrap.Context) (*redis.Client, func(), error) {
	cfg := ctx.GetConfig()
	if cfg == nil {
		return nil, func() {}, nil
	}

	l := ctx.NewLoggerHelper("redis/data/admin-service")

	cli := redisClient.NewClient(cfg.Data, klog.NewHelper(bLogger.AsKratosLogger(l)))

	return cli, func() {
		if err := cli.Close(); err != nil {
			l.Error(ctx.Context(), err.Error())
		}
	}, nil
}

// NewStorage 按 oss.storage 创建文件存储（minio 或 local）。
func NewStorage(ctx *bootstrap.Context) (oss.Storage, error) {
	return oss.NewStorage(ctx.GetConfig(), ctx.GetLogger())
}

func NewPasswordCrypto() password.Crypto {
	crypto, err := password.CreateCrypto("bcrypt")
	if err != nil {
		panic(err)
	}
	return crypto
}

func NewCaptcha(rdb *redis.Client) *captcha.Captcha {
	captchaInstance := captcha.NewCaptcha(rdb,
		captcha.WithDriverType(captcha.DriverMath),
		captcha.WithExpire(10*time.Minute),
		captcha.WithKeyPrefix(serviceid.ProjectName+":captcha"),
	)
	return captchaInstance
}

// NewExternalSources 按 data.external_sources 配置建外部数据源注册表
func NewExternalSources(ctx *bootstrap.Context) (*extdb.Registry, func(), error) {
	var cfgs map[string]*conf.Data_ExternalSource
	if cfg := ctx.GetConfig(); cfg != nil {
		cfgs = cfg.GetData().GetExternalSources()
	}
	return extdb.NewRegistry(ctx.Context(), ctx.NewLoggerHelper("external-source/data/admin-service"), cfgs)
}
