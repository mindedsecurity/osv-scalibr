// swiftutils/podfilelock.go
package swiftutils

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// PodfileLock represents the structure of a Podfile.lock file.
type PodfileLock struct {
	Pods            []interface{}     `yaml:"PODS"`
	SpecChecksums   map[string]string `yaml:"SPEC CHECKSUMS"`
	PodfileChecksum string            `yaml:"PODFILE CHECKSUM"`
	Cocopods        string            `yaml:"COCOAPODS"`
}

// Package represents a single package parsed from Podfile.lock.
type Package struct {
	Name    string
	Version string
}

// ParsePodfileLock parses the contents of a Podfile.lock and returns a list of packages.
func ParsePodfileLock(reader io.Reader) ([]Package, error) {
	bytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	var podfile PodfileLock
	if err = yaml.Unmarshal(bytes, &podfile); err != nil {
		return nil, fmt.Errorf("unable to parse YAML: %w", err)
	}

	var pkgs []Package
	for _, podInterface := range podfile.Pods {
		var podBlob string
		switch v := podInterface.(type) {
		case map[string]interface{}:
			for k := range v {
				podBlob = k
			}
		case string:
			podBlob = v
		default:
			return nil, fmt.Errorf("malformed Podfile.lock")
		}

		splits := strings.Split(podBlob, " ")
		if len(splits) < 2 {
			return nil, fmt.Errorf("unexpected format in Pods: %s", podBlob)
		}
		podName := splits[0]
		podVersion := strings.TrimSuffix(strings.TrimPrefix(splits[1], "("), ")")
		pkgs = append(pkgs, Package{
			Name:    podName,
			Version: podVersion,
		})
	}

	return pkgs, nil
}
