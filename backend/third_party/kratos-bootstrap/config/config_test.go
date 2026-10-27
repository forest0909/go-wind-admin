package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	kconfig "github.com/go-kratos/kratos/v2/config"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

// remoteOverridePath 供测试里的远程配置工厂读取。工厂只注册一次，路径每次测试更新。
var remoteOverridePath atomic.Value

func init() {
	const typeName Type = "overlaytestfile"
	if _, ok := GetFactory(typeName); ok {
		return
	}
	if err := RegisterFactory(typeName, func(*conf.RemoteConfig) (kconfig.Source, error) {
		path, _ := remoteOverridePath.Load().(string)
		if path == "" {
			return nil, os.ErrNotExist
		}
		return NewFileConfigSource(path), nil
	}); err != nil {
		panic(err)
	}
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func loadProvider(t *testing.T, dir string) kconfig.Config {
	t.Helper()
	cfg, err := NewConfigProvider(dir)
	if err != nil {
		t.Fatalf("NewConfigProvider: %v", err)
	}
	t.Cleanup(func() {
		if err := cfg.Close(); err != nil {
			t.Errorf("close config: %v", err)
		}
	})
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func configString(t *testing.T, cfg kconfig.Config, key string) string {
	t.Helper()
	got, err := cfg.Value(key).String()
	if err != nil {
		t.Fatalf("value %s: %v", key, err)
	}
	return got
}

func TestLocalOverride_NestedKeyWinsAndOmittedKeyKept(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "data.yaml"), `
data:
  database:
    driver: main-driver
    source: main-source
  redis:
    addr: main-redis
`)
	// 主配置旁边的其他子目录不能被读进来。file source 跳过子目录，覆盖层只认 local/。
	writeConfigFile(t, filepath.Join(dir, "extra", "data.yaml"), `
data:
  database:
    source: extra-source
  redis:
    addr: extra-redis
`)
	writeConfigFile(t, filepath.Join(dir, "local", "data.yaml"), `
data:
  database:
    source: local-source
`)

	cfg := loadProvider(t, dir)
	if got := configString(t, cfg, "data.database.source"); got != "local-source" {
		t.Fatalf("data.database.source = %q, want local-source", got)
	}
	if got := configString(t, cfg, "data.database.driver"); got != "main-driver" {
		t.Fatalf("data.database.driver = %q, want main-driver", got)
	}
	if got := configString(t, cfg, "data.redis.addr"); got != "main-redis" {
		t.Fatalf("data.redis.addr = %q, want main-redis", got)
	}
}

func TestNoLocalDir_MatchesMainSourceOnly(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "data.yaml"), `
data:
  database:
    driver: main-driver
    source: main-source
  redis:
    addr: main-redis
`)
	writeConfigFile(t, filepath.Join(dir, "extra", "data.yaml"), `
data:
  database:
    source: extra-source
`)

	got := loadProvider(t, dir)

	mainOnly := kconfig.New(kconfig.WithSource(NewFileConfigSource(dir)))
	t.Cleanup(func() {
		if err := mainOnly.Close(); err != nil {
			t.Errorf("close main only: %v", err)
		}
	})
	if err := mainOnly.Load(); err != nil {
		t.Fatalf("load main only: %v", err)
	}

	for _, key := range []string{"data.database.driver", "data.database.source", "data.redis.addr"} {
		if a, b := configString(t, got, key), configString(t, mainOnly, key); a != b {
			t.Fatalf("%s = %q via provider, %q via main source only", key, a, b)
		}
	}
	if got := configString(t, got, "data.database.source"); got != "main-source" {
		t.Fatalf("data.database.source = %q, want main-source", got)
	}
}

func TestRemoteConfigSourceStillLoadsAndLocalOverridesIt(t *testing.T) {
	dir := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.yaml")
	writeConfigFile(t, remote, `
data:
  database:
    driver: remote-driver
    source: remote-source
`)
	remoteOverridePath.Store(remote)

	writeConfigFile(t, filepath.Join(dir, "data.yaml"), `
data:
  database:
    driver: main-driver
    source: main-source
  redis:
    addr: main-redis
`)
	writeConfigFile(t, filepath.Join(dir, "config.yaml"), `
config:
  type: overlaytestfile
`)
	writeConfigFile(t, filepath.Join(dir, "local", "data.yaml"), `
data:
  database:
    source: local-source
`)

	cfg := loadProvider(t, dir)
	if got := configString(t, cfg, "data.database.driver"); got != "remote-driver" {
		t.Fatalf("data.database.driver = %q, want remote-driver", got)
	}
	if got := configString(t, cfg, "data.database.source"); got != "local-source" {
		t.Fatalf("data.database.source = %q, want local-source", got)
	}
	if got := configString(t, cfg, "data.redis.addr"); got != "main-redis" {
		t.Fatalf("data.redis.addr = %q, want main-redis", got)
	}
}

func configBool(t *testing.T, cfg kconfig.Config, key string) bool {
	t.Helper()
	got, err := cfg.Value(key).Bool()
	if err != nil {
		t.Fatalf("bool %s: %v", key, err)
	}
	return got
}

func configInt(t *testing.T, cfg kconfig.Config, key string) int64 {
	t.Helper()
	got, err := cfg.Value(key).Int()
	if err != nil {
		t.Fatalf("int %s: %v", key, err)
	}
	return got
}

// local 里显式写出的标量会覆盖主配置，包括空字符串、false 和 0。没写的键保留原值。
func TestLocalOverride_ExplicitZeroValuesReplaceMain(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "data.yaml"), `
data:
  database:
    driver: main-driver
    source: main-source
  redis:
    addr: main-redis
    enabled: true
    db: 7
`)
	writeConfigFile(t, filepath.Join(dir, "local", "data.yaml"), `
data:
  database:
    driver: ""
  redis:
    enabled: false
    db: 0
`)

	cfg := loadProvider(t, dir)
	if got := configString(t, cfg, "data.database.driver"); got != "" {
		t.Fatalf("data.database.driver = %q, want empty string", got)
	}
	if got := configString(t, cfg, "data.database.source"); got != "main-source" {
		t.Fatalf("data.database.source = %q, want main-source", got)
	}
	if got := configBool(t, cfg, "data.redis.enabled"); got {
		t.Fatal("data.redis.enabled = true, want false")
	}
	if got := configInt(t, cfg, "data.redis.db"); got != 0 {
		t.Fatalf("data.redis.db = %d, want 0", got)
	}
	if got := configString(t, cfg, "data.redis.addr"); got != "main-redis" {
		t.Fatalf("data.redis.addr = %q, want main-redis", got)
	}
}
