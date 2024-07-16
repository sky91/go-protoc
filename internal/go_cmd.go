package internal

import (
	"encoding/json"
	"github.com/pkg/errors"
	"os"
	"os/exec"
	"strings"
)

func GoListPkg(pkg string, tags []string) (*PackagePublic, *exec.Cmd, error) {
	args := []string{"list", "-json", "-e"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, pkg)
	cmd := exec.Command("go", args...)
	cmdOutput, err := cmd.Output()
	if err != nil {
		return nil, cmd, err
	}
	pkgInfo := &PackagePublic{}
	return pkgInfo, cmd, json.Unmarshal(cmdOutput, pkgInfo)
}

func GoListMod(mod string) (*ModulePublic, *exec.Cmd, error) {
	cmd := exec.Command("go", "list", "-json", "-m", "-e", mod)
	cmdOutput, err := cmd.Output()
	if err != nil {
		return nil, cmd, err
	}
	modInfo := &ModulePublic{}
	return modInfo, cmd, json.Unmarshal(cmdOutput, modInfo)
}

func GoInstall(pkg, installPath string) (*exec.Cmd, error) {
	cmd := exec.Command("go", "install", pkg)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if len(installPath) > 0 {
		cmd.Env = append(os.Environ(), "GOBIN="+installPath)
	}
	return cmd, cmd.Run()
}

func GoEnv(env string) (string, error) {
	cmd := exec.Command("go", "env", env)
	cmdOutput, err := cmd.Output()
	if err != nil {
		return "", errors.Wrap(err, "cmd.Output() error")
	}
	return strings.TrimSpace(string(cmdOutput)), nil
}
