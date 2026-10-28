package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nlewo/comin/internal/types"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

func Read(path string) (config types.Configuration, err error) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close() // nolint

	d := yaml.NewDecoder(file)
	if err := d.Decode(&config); err != nil {
		return config, err
	}
	for i, remote := range config.Fetcher.Git.Remotes {
		if remote.Auth.AccessTokenPath != "" {
			content, err := os.ReadFile(remote.Auth.AccessTokenPath)
			if err != nil {
				return config, err
			}
			config.Fetcher.Git.Remotes[i].Auth.AccessToken = strings.TrimSpace(string(content))
		}
		// On GitLab and GitHub, any non blank username is working
		if remote.Auth.Username == "" {
			config.Fetcher.Git.Remotes[i].Auth.Username = "comin"
		}
		if remote.Timeout == 0 {
			config.Fetcher.Git.Remotes[i].Timeout = 300
		}
		if remote.Branches.Main.Operation == "" {
			config.Fetcher.Git.Remotes[i].Branches.Main.Operation = "switch"
		}
		if remote.Branches.Testing.Operation == "" {
			config.Fetcher.Git.Remotes[i].Branches.Testing.Operation = "test"
		}

	}

	if config.ApiServer.ListenAddress == "" {
		config.ApiServer.ListenAddress = "127.0.0.1"
	}
	if config.ApiServer.Port == 0 {
		config.ApiServer.Port = 4242
	}
	if config.Exporter.ListenAddress == "" {
		config.Exporter.ListenAddress = "0.0.0.0"
	}
	if config.Exporter.Port == 0 {
		config.Exporter.Port = 4243
	}
	if config.StateFilepath == "" {
		config.StateFilepath = filepath.Join(config.StateDir, "state.json")
	}
	if config.Fetcher.Git.RepositorySubdir == "" {
		config.Fetcher.Git.RepositorySubdir = "."
	}
	if config.Fetcher.Type == "" {
		config.Fetcher.Type = "git"
	}
	supportedFetcherTypes := []string{"git", "niks3"}
	if !slices.Contains(supportedFetcherTypes, config.Fetcher.Type) {
		return config, fmt.Errorf("config: fetcher type is '%s' while it should be one of '%s'", config.Fetcher.Type, supportedFetcherTypes)
	}
	switch config.Fetcher.Type {
	case "git":
		supportedRepositoryTypes := []string{"flake", "nix"}
		if !slices.Contains(supportedRepositoryTypes, config.Fetcher.Git.RepositoryType) {
			return config, fmt.Errorf("config: repository type is '%s' while it be one of '%s'", config.Fetcher.Git.RepositoryType, supportedRepositoryTypes)
		}
	case "niks3":
		if len(config.Fetcher.Niks3.Remotes) == 0 {
			return config, fmt.Errorf("config: the niks3 fetcher requires at least one remote")
		}
		supportedOperations := []string{types.OperationSwitch, types.OperationTest, types.OperationBoot}
		for name, remote := range config.Fetcher.Niks3.Remotes {
			if remote.URL == "" {
				return config, fmt.Errorf("config: the niks3 remote '%s' has no url", name)
			}
			if remote.Operation == "" {
				remote.Operation = types.OperationSwitch
			}
			if !slices.Contains(supportedOperations, remote.Operation) {
				return config, fmt.Errorf("config: the niks3 remote '%s' has operation '%s' while it should be one of '%s'", name, remote.Operation, supportedOperations)
			}
			config.Fetcher.Niks3.Remotes[name] = remote
		}
	}
	if config.Grpc.UnixSocketPath == "" {
		config.Grpc.UnixSocketPath = filepath.Join(config.StateDir, "grpc.sock")
	}
	if config.EvalTimeout == 0 {
		config.EvalTimeout = 1800
	}
	if config.BuildTimeout == 0 {
		config.BuildTimeout = 1800
	}
	if config.BuildRetryTimer == 0 {
		config.BuildRetryTimer = 30
	}
	logrus.Debugf("Config is '%#v'", config)
	return
}

func MkGitConfig(config types.Configuration) types.GitConfig {
	return types.GitConfig{
		Path:                  filepath.Join(config.StateDir, "repository"),
		Dir:                   config.Fetcher.Git.RepositorySubdir,
		Remotes:               config.Fetcher.Git.Remotes,
		GpgPublicKeyPaths:     config.Fetcher.Git.GpgPublicKeyPaths,
		SshAllowedSignersPath: config.Fetcher.Git.SshAllowedSignersPath,
		Submodules:            config.Fetcher.Git.Submodules,
		SystemAttr:            config.Fetcher.Git.SystemAttr,
	}
}
