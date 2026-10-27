package config

import (
	"os"
	"path/filepath"

	"github.com/go-kratos/kratos/v2/config"
	fileKratos "github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"

	conf "go-wind-admin/third_party/kratos-bootstrap/api/gen/go/conf/v1"
)

const remoteConfigSourceConfigFile = "config.yaml"

// NewFileConfigSource 创建一个本地文件配置源
func NewFileConfigSource(filePath string) config.Source {
	return fileKratos.NewSource(filePath)
}

// NewConfigProvider 创建一个配置
func NewConfigProvider(configPath string) (config.Config, error) {
	err, rc := LoadRemoteConfigSourceConfigs(configPath)
	if err != nil {
		log.Error("LoadRemoteConfigSourceConfigs: ", err.Error())
		return nil, err
	}

	// 主配置目录在前。file source 读目录时跳过子目录，所以后面的 local/ 不会被主源再读一遍。
	// 远程源（config.yaml）仍插在主配置之后、local 之前，没有 config.yaml 时这条分支不走。
	// local 最后加载。Kratos 用 mergo.WithOverride 做深度合并。
	// local 里显式写出的标量都会覆盖同名键，包括空字符串、false 和 0。没写的键保留前面的值。
	sources := []config.Source{NewFileConfigSource(configPath)}
	if rc != nil {
		rcs, err := NewProvider(rc)
		if err != nil {
			log.Error("NewProvider: ", err.Error())
			return nil, err
		}
		sources = append(sources, rcs)
	}
	sources, err = appendLocalOverrideSource(sources, configPath)
	if err != nil {
		log.Error("appendLocalOverrideSource: ", err.Error())
		return nil, err
	}

	return config.New(config.WithSource(sources...)), nil
}

// localOverrideDir 是主配置目录下的可选覆盖层，只在目录存在时加载。
const localOverrideDir = "local"

// appendLocalOverrideSource 把 <configPath>/local 追加到源列表末尾。
// 目录不存在，或该路径不是目录时，源列表保持原样。
func appendLocalOverrideSource(sources []config.Source, configPath string) ([]config.Source, error) {
	localPath := filepath.Join(configPath, localOverrideDir)
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sources, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return sources, nil
	}
	return append(sources, NewFileConfigSource(localPath)), nil
}

// LoadBootstrapConfig 加载程序引导配置
func LoadBootstrapConfig(configPath string) error {
	cfg, err := NewConfigProvider(configPath)
	if err != nil {
		return err
	}

	if err = cfg.Load(); err != nil {
		return err
	}

	initBootstrapConfig()

	if err = scanConfigs(cfg); err != nil {
		return err
	}

	return nil
}

func scanConfigs(cfg config.Config) error {
	initBootstrapConfig()

	for _, c := range configList {
		if err := cfg.Scan(c); err != nil {
			return err
		}
	}
	return nil
}

// LoadRemoteConfigSourceConfigs 加载远程配置源的本地配置
func LoadRemoteConfigSourceConfigs(configPath string) (error, *conf.RemoteConfig) {
	configPath = filepath.Join(configPath, remoteConfigSourceConfigFile)
	if !pathExists(configPath) {
		return nil, nil
	}

	cfg := config.New(
		config.WithSource(
			NewFileConfigSource(configPath),
		),
	)
	defer func(cfg config.Config) {
		if err := cfg.Close(); err != nil {
			panic(err)
		}
	}(cfg)

	var err error

	if err = cfg.Load(); err != nil {
		return err, nil
	}

	if err = scanConfigs(cfg); err != nil {
		return err, nil
	}

	return nil, GetBootstrapConfig().Config
}

// pathExists 判断路径是否存在
func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}
