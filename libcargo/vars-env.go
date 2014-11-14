package cargo

import (
	"os"
	"strings"
)

type providerEnv struct {
}

func (p *providerEnv) QueryVar(name string, context *VarContext, repo VarRepository) (string, bool) {
	if strings.HasPrefix(name, "env:") {
		return os.Getenv(name[4:]), true
	}
	return "", true
}

func providerFactoryEnv() VarProvider {
	return &providerEnv{}
}

func init() {
	VarProviders["env"] = providerFactoryEnv
}
