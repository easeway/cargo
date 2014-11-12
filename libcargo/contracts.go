package cargo

type Cluster struct {
	Name  string
	Nodes []Node
}

type DockerProperties struct {
	Entrypoint string   `json:"entrypoint"`
	Cmd        []string `json:"cmd"`
	Env        []string `json:"env"`
	Privileged bool     `json:"privileged"`
	Volumes    []string `json:"volumes"`
}

type Commands struct {
	Files    []string `json:"files"`
	Env      []string `json:"env"`
	WorkDir  string   `json:"workdir"`
	Commands []string `json:"commands"`
}

type CaptureFile struct {
	Local  string
	Remote string
}

type Capture struct {
	Files []CaptureFile
}

type Node struct {
	Name      string
	Instances uint
	Image     string
	Docker    DockerProperties
	Commands  map[string]*Commands
	Capture   Capture
}

type Clusters struct {
	Clusters []Cluster
	Default  string
}
