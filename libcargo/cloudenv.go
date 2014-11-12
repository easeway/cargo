package cargo

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
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
	State    *CloudState
	Name     string
	Complete bool
	Logger   Logger
	Error    error
}

type CloudState struct {
	Env       *CloudEnv
	Images    map[string]*ImageLoader
	Nodes     []NodeState
	WaitGroup sync.WaitGroup

	stateDir  string
	workspace string
	lock      sync.Mutex
	cond      *sync.Cond
}

type NodeState struct {
	State      *CloudState
	Node       *Node
	Image      string
	DockerArgs []string
	Instances  []InstanceState
	Logger     Logger
	Error      error
}

type InstanceState struct {
	NodeState   *NodeState
	Index       uint
	ContainerId string
	Logger      Logger
	Error       error
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
		for !loader.Complete {
			cs.Wait()
		}
		cs.Unlock()
	} else {
		loader = &ImageLoader{State: cs, Name: name}
		loader.Logger = cs.Env.Logger.NewLogger(name)
		cs.Images[name] = loader
		cs.Unlock()
		loader.Load()
		cs.Notify()
	}
	return loader.Error
}

func (cs *CloudState) Substitute(text string) string {
	// TODO
	return text
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
	il.Complete = true
}

func (ce *CloudEnv) Run() *CloudState {
	cs := &CloudState{
		Env:    ce,
		Images: make(map[string]*ImageLoader),
		Nodes:  make([]NodeState, len(ce.Cluster.Nodes)),
	}

	cs.stateDir = path.Join(ce.DataDir, states, ce.Cluster.Name)
	cs.workspace = path.Join(workspace, states, ce.Cluster.Name)

	cs.cond = sync.NewCond(&cs.lock)

	ce.Logger.Info("Starting cluster %s", ce.Cluster.Name)

	var wg sync.WaitGroup
	for i := 0; i < len(cs.Nodes); i++ {
		cs.Nodes[i].Node = &ce.Cluster.Nodes[i]
		wg.Add(1)
		go func(ns *NodeState) {
			if ns.Error = ns.run(cs); ns.Error != nil {
				ns.Logger.Error("%v", ns.Error)
			}
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

	ns.State = cs
	ns.Image = cs.Substitute(ns.Node.Image)
	ns.DockerArgs = []string{"-v", cs.Env.DataDir + ":" + workspace, "-w", workspace}
	ns.Instances = make([]InstanceState, ns.Node.Instances)
	ns.Logger = cs.Env.Logger.NewLogger(ns.Node.Name)

	if err := cs.LoadImage(ns.Node.Image); err != nil {
		return err
	}

	if ns.Node.Docker.Privileged {
		ns.DockerArgs = append(ns.DockerArgs, "--privileged")
	}
	if ns.Node.Docker.Entrypoint != "" {
		ns.DockerArgs = append(ns.DockerArgs, "--entrypoint")
		ns.DockerArgs = append(ns.DockerArgs, cs.Substitute(ns.Node.Docker.Entrypoint))
	}
	for _, env := range ns.Node.Docker.Env {
		ns.DockerArgs = append(ns.DockerArgs, "-e")
		ns.DockerArgs = append(ns.DockerArgs, cs.Substitute(env))
	}
	for _, vol := range ns.Node.Docker.Volumes {
		volMap := cs.Substitute(vol)
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
	cidfile := path.Join(ns.State.stateDir, name+".cid")
	runDir := path.Join(ns.State.stateDir, name+".run")
	localScript := path.Join(runDir, "cmd.sh")
	remoteScript := path.Join(ns.State.workspace, name+".run", "cmd.sh")
	remoteWrapper := path.Join(ns.State.workspace, name+".run", "run.sh")

	is.NodeState = ns
	is.Index = index
	is.Logger = ns.Logger.NewLogger(name)

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
	if is.ContainerId, err = is.docker().Create(cidfile, ns.DockerArgs); err != nil {
		return err
	}

	if (ns.State.Env.RunFlags & Detach) != 0 {
		err = is.docker().Start(is.ContainerId, nil)
	} else {
		err = is.docker().Start(is.ContainerId, &ns.State.WaitGroup)
	}
	if err != nil {
		return err
	}

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
	// TODO env, workdir
	for _, command := range commands.Commands {
		command = is.NodeState.State.Substitute(command)
		if command = strings.Trim(command, " "); command == "" {
			continue
		}
		is.Logger.Info("RUN %s", command)
		if err := os.Remove(localScript + ".exit"); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := ioutil.WriteFile(localScript, []byte("#!/bin/sh\n"+command), 0777); err != nil {
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
			return errors.New(fmt.Sprintf("Exit %v: %s", exitCode, command))
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
		is.docker().Remove(is.ContainerId)
	}
}
