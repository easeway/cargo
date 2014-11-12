package cargo

import (
	"encoding/json"
	"errors"
	"github.com/easeway/go-dynobj"
	"io/ioutil"
	"path"
)

var (
	errorClusterNoName       = errors.New("Cluster name not defined")
	errorClusterNoNodes      = errors.New("Cluster nodes not defined")
	errorClusterBadNode      = errors.New("Bad node definition")
	errorClusterBadInstances = errors.New("Bad instances value")
)

func (cs *Clusters) DefaultCluster() *Cluster {
	if len(cs.Clusters) == 0 {
		return nil
	}
	if cs.Default == "" {
		return &cs.Clusters[0]
	}
	return cs.ClusterByName(cs.Default)
}

func (cs *Clusters) ClusterByName(name string) *Cluster {
	for _, cluster := range cs.Clusters {
		if cluster.Name == name {
			return &cluster
		}
	}
	return nil
}

func LoadYaml(filename string) (*Clusters, error) {
	if data, err := ioutil.ReadFile(filename); err != nil {
		return nil, err
	} else if raw, err := yamlDecode(data); err != nil {
		return nil, err
	} else {
		clusters := &Clusters{}
		obj := &dynobj.DynObj{Query: &dynobj.DynQuery{Object: raw}}
		arr := obj.AsAny("clusters")
		if objs, ok := arr.([]interface{}); ok {
			clusters.Clusters = make([]Cluster, len(objs))
			for index, clusterDef := range objs {
				cluster := &clusters.Clusters[index]
				if err := decodeCluster(clusterDef, cluster); err != nil {
					return nil, err
				} else if cluster.Name == "" {
					return nil, errorClusterNoName
				}
			}
			clusters.Default = obj.AsStr("default")
		} else {
			clusters.Clusters = make([]Cluster, 1)
			cluster := &clusters.Clusters[0]
			if err := decodeCluster(raw, cluster); err != nil {
				return nil, err
			}
			// derive name from file name
			if cluster.Name == "" {
				cluster.Name = path.Base(filename)
			}
		}
		return clusters, nil
	}
}

func decodeCluster(raw interface{}, cluster *Cluster) error {
	obj := &dynobj.DynObj{Query: &dynobj.DynQuery{Object: raw}}
	cluster.Name = obj.AsStr("name")
	nodesObj := obj.AsAny("nodes")
	if nodesArr, ok := nodesObj.([]interface{}); ok {
		cluster.Nodes = make([]Node, len(nodesArr))
		for index, nodeObj := range nodesArr {
			if err := decodeNode(nodeObj, &cluster.Nodes[index]); err != nil {
				return err
			}
		}
	} else {
		return errorClusterNoNodes
	}
	return nil
}

func decodeNode(raw interface{}, node *Node) error {
	nodeMap, ok := raw.(map[string]interface{})
	if !ok {
		return errorClusterBadNode
	}

	node.Commands = make(map[string]*Commands)
	if node.Name, ok = nodeMap["name"].(string); !ok || node.Name == "" {
		return errorClusterBadNode
	}
	if node.Image, ok = nodeMap["image"].(string); !ok || node.Image == "" {
		return errorClusterBadNode
	}
	if instancesObj, exists := nodeMap["instances"]; !exists {
		node.Instances = 1
	} else if instances, ok := instancesObj.(uint); !ok {
		return errorClusterBadInstances
	} else {
		node.Instances = instances
	}

	if err := decodeCommands(nodeMap, "prepare", node.Commands); err != nil {
		return err
	}
	if err := decodeCommands(nodeMap, "run", node.Commands); err != nil {
		return err
	}

	if err := decodeDockerProperties(nodeMap["docker"], &node.Docker); err != nil {
		return err
	}

	if err := decodeCapture(nodeMap["capture"], &node.Capture); err != nil {
		return err
	}

	return nil
}

func decodeCommands(nodeMap map[string]interface{},
	name string,
	cmds map[string]*Commands) error {
	cmd := &Commands{}
	if cmdsObj, exists := nodeMap[name]; !exists {
		return nil
	} else if err := unmarshal(cmdsObj, cmd); err != nil {
		return err
	}
	cmds[name] = cmd
	return nil
}

func decodeDockerProperties(obj interface{}, prop *DockerProperties) error {
	if obj == nil {
		return nil
	}
	return unmarshal(obj, prop)
}

func decodeCapture(raw interface{}, capture *Capture) error {
	if raw == nil {
		return nil
	}
	if obj, ok := raw.(map[string]interface{}); !ok {
		return errorClusterBadNode
	} else if filesRaw, exists := obj["files"]; !exists {
		return nil
	} else if files, ok := filesRaw.([]interface{}); !ok {
		return errorClusterBadNode
	} else {
		capture.Files = make([]CaptureFile, len(files))
		for index, fileObj := range files {
			if fn, ok := fileObj.(string); ok {
				capture.Files[index].Local = fn
				capture.Files[index].Remote = fn
			} else if names, ok := fileObj.(map[string]interface{}); ok {
				local, localOk := names["local"].(string)
				remote, remoteOk := names["remote"].(string)
				if !localOk || !remoteOk {
					return errorClusterBadNode
				}
				capture.Files[index].Local = local
				capture.Files[index].Remote = remote
			} else {
				return errorClusterBadNode
			}
		}
	}
	return nil
}

func unmarshal(obj interface{}, out interface{}) error {
	if encoded, err := json.Marshal(obj); err != nil {
		return err
	} else if err := json.Unmarshal(encoded, out); err != nil {
		return err
	}
	return nil
}
