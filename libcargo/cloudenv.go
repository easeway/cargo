package cargo

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	Create  = 0x0001
	Prepare = 0x0002
	Run     = 0x0004
	Detach  = 0x0008
	Stop    = 0x0010
	Remove  = 0x0020

	workspace = "/.cargo.workspace"
	states    = ".cargo"
)

type Logger interface {
	NewLogger(name string) Logger
	Critical(fmt string, args ...interface{})
	Error(fmt string, args ...interface{})
	Warning(fmt string, args ...interface{})
	Info(fmt string, args ...interface{})
	Debug(fmt string, args ...interface{})
	Trace(fmt string, args ...interface{})
}

type CloudEnv struct {
	Cluster  *Cluster
	DataDir  string
	Registry string
	Logger   Logger
	RunFlags uint
}

type ImageLoader struct {
	State  *CloudState
	Name   string
	Logger Logger
	Error  error

	complete int32
}

type CloudState struct {
	Env       *CloudEnv
	Images    map[string]*ImageLoader
	Nodes     []NodeState
	WaitGroup sync.WaitGroup

	stateDir  string
	workspace string
	vars      VarRepository

	lock sync.Mutex
	cond *sync.Cond
}

type NodeState struct {
	State      *CloudState
	Node       *Node
	Image      string
	LocalVars  VarRepository
	DockerArgs []string
	Instances  []InstanceState
	Logger     Logger
	Error      error
	Stopped    bool
}

type InstanceState struct {
	NodeState   *NodeState
	Index       uint
	ContainerId string
	LocalVars   VarRepository
	Logger      Logger
	Error       error
	Stopped     bool

	cidfile string
}

func (cs *CloudState) Lock() {
	cs.lock.Lock()
}

func (cs *CloudState) Unlock() {
	cs.lock.Unlock()
}

func (cs *CloudState) Wait() {
	cs.cond.Wait()
}

func (cs *CloudState) Notify() {
	cs.cond.Broadcast()
}

func (cs *CloudState) LoadImage(name string) error {
	cs.Lock()
	loader, exists := cs.Images[name]
	if exists {
		for atomic.LoadInt32(&loader.complete) == 0 {
			cs.Wait()
		}
		cs.Unlock()
	} else {
		loader = &ImageLoader{State: cs, Name: name}
		loader.Logger = cs.Env.Logger.NewLogger(name)
		cs.Images[name] = loader
		cs.Unlock()
		loader.Load()
		atomic.StoreInt32(&loader.complete, 1)
		cs.Notify()
	}
	return loader.Error
}

var varRegExp = regexp.MustCompilePOSIX("%\\([^()]+\\)")

func (cs *CloudState) Substitute(text string, context *VarContext) string {
	result := ""
	curpos := 0
	for _, index := range varRegExp.FindAllStringIndex(text, -1) {
		start := index[0]
		end := index[1]
		if start > curpos {
			result += text[curpos:start]
		}
		curpos = end + 1
		name := text[start+2 : end-1]
		if val, exists := cs.vars.QueryVar(name, context); exists {
			result += val
		}
	}
	if curpos < len(text) {
		result += text[curpos:]
	}
	return result
}

func (cs *CloudState) AnyError() bool {
	for i := 0; i < len(cs.Nodes); i++ {
		if cs.Nodes[i].Error != nil || cs.Nodes[i].AnyError() {
			return true
		}
	}
	return false
}

func (cs *CloudState) StopAndWait() {
	for i := 0; i < len(cs.Nodes); i++ {
		ns := &cs.Nodes[i]
		for j := 0; j < len(ns.Instances); j++ {
			ns.Instances[j].stop()
		}
	}
	cs.WaitGroup.Wait()
}

func (il *ImageLoader) Load() {
	il.Logger.Info("Loading image")
	il.Error = Docker(il.State.Env, il.Logger).Pull(il.Name)
}

func (ce *CloudEnv) Run() *CloudState {
	cs := &CloudState{
		Env:    ce,
		Images: make(map[string]*ImageLoader),
		Nodes:  make([]NodeState, len(ce.Cluster.Nodes)),
		vars:   GlobalVarsRepo(),
	}

	cs.stateDir = path.Join(ce.DataDir, states, ce.Cluster.Name)
	cs.workspace = path.Join(workspace, states, ce.Cluster.Name)

	cs.cond = sync.NewCond(&cs.lock)

	cs.vars.UpdateVar("project", ce.Cluster.Name)
	cs.vars.UpdateVar("cluster", ce.Cluster.Name)
	cs.vars.UpdateVar("container", "docker")
	cs.vars.UpdateVar("os", "linux")

	ce.Logger.Info("Starting cluster %s", ce.Cluster.Name)

	var wg sync.WaitGroup
	for i := 0; i < len(cs.Nodes); i++ {
		ns := &cs.Nodes[i]
		ns.State = cs
		ns.Node = &ce.Cluster.Nodes[i]
		ns.LocalVars = LocalVarsRepo()
		ns.Instances = make([]InstanceState, ns.Node.Instances)
		for j := 0; j < len(ns.Instances); j++ {
			is := &ns.Instances[j]
			is.NodeState = ns
			is.Index = uint(j)
			is.LocalVars = LocalVarsRepo()
		}
		wg.Add(1)
	}
	for i := 0; i < len(cs.Nodes); i++ {
		go func(ns *NodeState) {
			if ns.Error = ns.run(cs); ns.Error != nil {
				ns.Logger.Error("%v", ns.Error)
			}
			ns.Stopped = true
			cs.Notify()
			wg.Done()
		}(&cs.Nodes[i])
	}
	wg.Wait()
	return cs
}

func (ns *NodeState) run(cs *CloudState) error {
	if err := os.MkdirAll(cs.stateDir, 0777); err != nil && !os.IsExist(err) {
		return err
	}

	varCtx := &VarContext{Cloud: ns.State, Node: ns}

	ns.LocalVars.UpdateVar("template", ns.Node.Name)
	ns.LocalVars.UpdateVar("instances", fmt.Sprintf("%v", len(ns.Instances)))
	ns.Image = cs.Substitute(ns.Node.Image, varCtx)
	ns.LocalVars.UpdateVar("image", ns.Image)
	cs.Notify()

	ns.DockerArgs = []string{"-v", cs.Env.DataDir + ":" + workspace, "-w", workspace}
	ns.Logger = cs.Env.Logger.NewLogger(ns.Node.Name)

	if err := cs.LoadImage(ns.Node.Image); err != nil {
		return err
	}

	if ns.Node.Docker.Privileged {
		ns.DockerArgs = append(ns.DockerArgs, "--privileged")
	}
	if ns.Node.Docker.Entrypoint != "" {
		ns.DockerArgs = append(ns.DockerArgs, "--entrypoint")
		ns.DockerArgs = append(ns.DockerArgs, cs.Substitute(ns.Node.Docker.Entrypoint, varCtx))
	}
	for _, env := range ns.Node.Docker.Env {
		ns.DockerArgs = append(ns.DockerArgs, "-e")
		ns.DockerArgs = append(ns.DockerArgs, cs.Substitute(env, varCtx))
	}
	for _, vol := range ns.Node.Docker.Volumes {
		volMap := cs.Substitute(vol, varCtx)
		if pos := strings.Index(volMap, ":"); pos > 0 {
			src := volMap[0:pos]
			dst := volMap[pos+1:]
			if !path.IsAbs(src) {
				src = path.Clean(path.Join(cs.Env.DataDir, src))
			}
			volMap = src + ":" + dst
		}

		ns.DockerArgs = append(ns.DockerArgs, "-v")
		ns.DockerArgs = append(ns.DockerArgs, volMap)
	}

	if (ns.State.Env.RunFlags & Prepare) != 0 {
		if err := ns.prepareNode(); err != nil {
			return err
		}
	}

	ns.DockerArgs = append(ns.DockerArgs, ns.Image)
	for _, cmd := range ns.Node.Docker.Cmd {
		ns.DockerArgs = append(ns.DockerArgs, cmd)
	}

	var wg sync.WaitGroup
	for i := 0; i < len(ns.Instances); i++ {
		wg.Add(1)
		go func(index uint, is *InstanceState) {
			if is.Error = is.run(ns, index); is.Error != nil {
				is.Logger.Error("%v", is.Error)
			}
			is.Stopped = true
			cs.Notify()
			wg.Done()
		}(uint(i), &ns.Instances[i])
	}
	wg.Wait()

	return nil
}

func (ns *NodeState) prepareNode() error {
	// TODO
	return nil
}

func (ns *NodeState) AnyError() bool {
	for i := 0; i < len(ns.Instances); i++ {
		if ns.Instances[i].Error != nil {
			return true
		}
	}
	return false
}

func (is *InstanceState) run(ns *NodeState, index uint) (err error) {
	name := fmt.Sprintf("%s.%v", ns.Node.Name, index)
	runDir := path.Join(ns.State.stateDir, name+".run")
	localScript := path.Join(runDir, "cmd.sh")
	remoteScript := path.Join(ns.State.workspace, name+".run", "cmd.sh")
	remoteWrapper := path.Join(ns.State.workspace, name+".run", "run.sh")

	is.Logger = ns.Logger.NewLogger(name)
	is.cidfile = path.Join(ns.State.stateDir, name+".cid")

	runCmds := (ns.State.Env.RunFlags & Run) != 0
	if runCmds {
		if err := os.MkdirAll(runDir, 0777); err != nil {
			return err
		}
		if err := ioutil.WriteFile(path.Join(runDir, "run.sh"),
			[]byte("#!/bin/sh\n"+
				remoteScript+"\necho $?>"+remoteScript+".exit\n"+
				"chmod 0777 "+remoteScript+".exit"), 0777); err != nil {
			return err
		}
	}
	is.Logger.Info("Spawning instance")
	if is.ContainerId, err = is.docker().Create(is.cidfile, ns.DockerArgs); err != nil {
		return err
	}

	if (ns.State.Env.RunFlags & Detach) != 0 {
		err = is.docker().Start(is.ContainerId, nil)
	} else {
		err = is.docker().Start(is.ContainerId, &ns.State.WaitGroup)
	}
	if err != nil {
		is.remove()
		return err
	}

	if ip, err := is.docker().Inspect(is.ContainerId, "{{.NetworkSettings.IPAddress}}"); err == nil {
		is.LocalVars.UpdateVar("ip", ip)
	}
	if mac, err := is.docker().Inspect(is.ContainerId, "{{.NetworkSettings.MacAddress}}"); err == nil {
		is.LocalVars.UpdateVar("mac", mac)
	}
	is.NodeState.State.Notify()

	if runCmds {
		if err = is.runCommands("run", remoteWrapper, localScript, remoteScript); err == nil {
			err = is.capture()
		}
	}

	if (ns.State.Env.RunFlags & Stop) != 0 {
		is.stop()
		if (ns.State.Env.RunFlags & Remove) != 0 {
			is.remove()
		}
	}
	return
}

func (is *InstanceState) docker() *docker {
	return Docker(is.NodeState.State.Env, is.Logger)
}

func (is *InstanceState) runCommands(name, remoteWrapper, localScript, remoteScript string) error {
	commands, exists := is.NodeState.Node.Commands[name]
	if !exists {
		return nil
	}

	varCtx := &VarContext{Cloud: is.NodeState.State, Node: is.NodeState, Instance: is}
	shell := "/bin/bash"
	if commands.Shell != "" {
		shell = commands.Shell
	}
	for _, command := range commands.Commands {
		command = is.NodeState.State.Substitute(command, varCtx)
		if command = strings.Trim(command, " "); command == "" {
			continue
		}
		is.Logger.Info("RUN %s", command)
		if err := os.Remove(localScript + ".exit"); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := ioutil.WriteFile(localScript, []byte("#!"+shell+"\n"+command), 0777); err != nil {
			return err
		}
		if err := is.docker().Exec(is.ContainerId, remoteWrapper); err != nil {
			return err
		}
		if result, err := ioutil.ReadFile(localScript + ".exit"); err != nil {
			return err
		} else if exitCode, err := strconv.Atoi(strings.Trim(string(result), " \n\r\t\f")); err != nil {
			return err
		} else if exitCode != 0 {
			err := errors.New(fmt.Sprintf("Exit %v: %s", exitCode, command))
			is.Logger.Error("ERR %v", err)
			return err
		}
	}
	return nil
}

func (is *InstanceState) capture() error {
	// TODO
	return nil
}

func (is *InstanceState) stop() {
	if is.ContainerId != "" {
		is.Logger.Info("Stopping")
		is.docker().Stop(is.ContainerId)
	}
}

func (is *InstanceState) remove() {
	if is.ContainerId != "" {
		is.Logger.Info("Removing")
		is.docker().RmForce(is.ContainerId)
		if is.cidfile != "" {
			if os.Remove(is.cidfile) == nil {
				is.cidfile = ""
			}
		}
		is.ContainerId = ""
	}
}
