package cargo

import (
	"strconv"
	"strings"
)

type providerXref struct {
}

func (p *providerXref) QueryVar(name string, context *VarContext, repo VarRepository) (val string, exists bool) {
	// ensure prefix is valid
	pos := strings.Index(name, ":")
	if pos <= 0 {
		return
	}

	key := name[0:pos]
	ref := name[pos+1:]
	switch key {
	case "instances":
		return queryNode(context, ref, key)
	case "ip", "mac":
		return queryInstance(context, ref, key)
	}
	return
}

func providerFactoryXref() VarProvider {
	return &providerXref{}
}

func queryNode(ctx *VarContext, ref, key string) (val string, exists bool) {
	ns := findNode(ctx, ref)
	if ns == nil {
		return
	}

	if val, exists = ns.LocalVars.QueryVar(key, ctx); exists || ns == ctx.Node {
		return
	}

	ctx.Cloud.Lock()
	for !ns.Stopped {
		if val, exists = ns.LocalVars.QueryVar(key, ctx); exists {
			break
		}
		ctx.Cloud.Wait()
	}
	ctx.Cloud.Unlock()
	return
}

func queryInstance(ctx *VarContext, ref, key string) (val string, exists bool) {
	pos := strings.LastIndex(ref, "-")
	if pos <= 0 {
		return
	}
	ns := findNode(ctx, ref[0:pos])
	if ns == nil {
		return
	}
	index, err := strconv.Atoi(ref[pos+1:])
	if err != nil || index < 0 || index >= len(ns.Instances) {
		return
	}

	is := &ns.Instances[index]

	if val, exists = is.LocalVars.QueryVar(key, ctx); exists || is == ctx.Instance {
		return
	}

	ctx.Cloud.Lock()
	for !is.Stopped {
		if val, exists = is.LocalVars.QueryVar(key, ctx); exists {
			break
		}
		ctx.Cloud.Wait()
	}
	ctx.Cloud.Unlock()
	return
}

func findNode(ctx *VarContext, name string) *NodeState {
	for i := 0; i < len(ctx.Cloud.Nodes); i++ {
		if ctx.Cloud.Nodes[i].Node.Name == name {
			return &ctx.Cloud.Nodes[i]
		}
	}
	return nil
}

func init() {
	VarProviders["instances"] = providerFactoryXref
	VarProviders["ip"] = providerFactoryXref
	VarProviders["mac"] = providerFactoryXref
}
