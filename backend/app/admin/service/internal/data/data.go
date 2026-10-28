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

	"go-wind-admin/pkg/oss"
	"go-wind-admin/pkg/serviceid"
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

func NewMinIoClient(ctx *bootstrap.Context) *oss.MinIOClient {
	return oss.NewMinIoClient(ctx.GetConfig(), ctx.GetLogger())
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
