package cargo

import (
	"strings"
	"sync"
)

type VarContext struct {
	Cloud    *CloudState
	Node     *NodeState
	Instance *InstanceState
}

type VarRepository interface {
	QueryVar(name string, context *VarContext) (string, bool)
	UpdateVar(name, value string)
}

type VarProvider interface {
	QueryVar(name string, context *VarContext, repo VarRepository) (string, bool)
}

type VarProviderFactory func() VarProvider

var (
	VarProviders = make(map[string]VarProviderFactory)
)

type localVars struct {
	values map[string]string
	lock   sync.RWMutex
}

func LocalVarsRepo() VarRepository {
	return &localVars{values: make(map[string]string)}
}

func (vs *localVars) QueryVar(name string, context *VarContext) (val string, exists bool) {
	vs.lock.RLock()
	defer vs.lock.RUnlock()
	val, exists = vs.values[name]
	return
}

func (vs *localVars) UpdateVar(name, value string) {
	vs.lock.Lock()
	vs.values[name] = value
	vs.lock.Unlock()
}

type globalVars struct {
	localVars
	providers map[string]VarProvider
}

func GlobalVarsRepo() VarRepository {
	repo := &globalVars{
		localVars: localVars{values: make(map[string]string)},
		providers: make(map[string]VarProvider),
	}
	for name, factory := range VarProviders {
		repo.providers[name] = factory()
	}
	return repo
}

func (vs *globalVars) QueryVar(name string, context *VarContext) (val string, exists bool) {
	vs.localVars.lock.RLock()
	val, exists = vs.localVars.values[name]
	vs.localVars.lock.RUnlock()
	if !exists {
		pos := strings.Index(name, ":")
		if pos > 0 {
			if provider, ok := vs.providers[name[0:pos]]; ok {
				val, exists = provider.QueryVar(name, context, vs)
			}
		}
	}
	return
}

func (vs *globalVars) UpdateVar(name, value string) {
	vs.localVars.UpdateVar(name, value)
}
