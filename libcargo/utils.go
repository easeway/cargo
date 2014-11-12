package cargo

import (
	"fmt"
	"gopkg.in/yaml.v1"
)

func yamlDecode(data []byte) (interface{}, error) {
	var in interface{}
	if err := yaml.Unmarshal(data, &in); err != nil {
		return nil, err
	}
	return yamlFix(in)
}

func yamlFix(in interface{}) (interface{}, error) {
	switch in.(type) {
	case map[interface{}]interface{}:
		o := make(map[string]interface{})
		for k, v := range in.(map[interface{}]interface{}) {
			if val, err := yamlFix(v); err != nil {
				return nil, err
			} else {
				o[fmt.Sprintf("%v", k)] = val
			}
		}
		return o, nil
	case []interface{}:
		array := in.([]interface{})
		l := len(array)
		o := make([]interface{}, l)
		for i := 0; i < l; i++ {
			if val, err := yamlFix(array[i]); err != nil {
				return nil, err
			} else {
				o[i] = val
			}
		}
		return o, nil
	}
	return in, nil
}
