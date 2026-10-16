package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/norlinga/mason/internal/config"
	"gopkg.in/yaml.v3"
)

// UserConfigValues are the values setup records. Empty fields are left as they are.
type UserConfigValues struct {
	Server    string
	Key       string
	ShareURL  string
	SharePath string
}

// UserConfigWrite reports what WriteUserConfig did.
type UserConfigWrite struct {
	Changed bool
	// Backup is the path the previous v1 file was moved to, if any.
	Backup string
}

// WriteUserConfig merges v into the user config at path.
func WriteUserConfig(path string, v UserConfigValues) (UserConfigWrite, error) {
	var res UserConfigWrite
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("read %s: %w", path, err)
	}

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	isV1 := false
	if len(bytes.TrimSpace(original)) > 0 {
		var parsed yaml.Node
		if err := yaml.Unmarshal(original, &parsed); err != nil {
			return res, fmt.Errorf("parse %s: %w", path, err)
		}
		if len(parsed.Content) == 1 && parsed.Content[0].Kind == yaml.MappingNode {
			if mapValue(parsed.Content[0], "targets") != nil || mapValue(parsed.Content[0], "default") != nil {
				isV1 = true
			} else {
				doc = &parsed
			}
		}
	}
	root := doc.Content[0]

	setScalar(root, "server", v.Server)
	setScalar(root, "key", v.Key)
	if v.ShareURL != "" {
		share := mapValue(root, "share")
		switch {
		case share != nil && share.Kind == yaml.MappingNode:
			setScalar(share, "url", v.ShareURL)
			setScalar(share, "path", v.SharePath)
		case v.SharePath != "":
			m := &yaml.Node{Kind: yaml.MappingNode}
			setScalar(m, "url", v.ShareURL)
			setScalar(m, "path", v.SharePath)
			setNode(root, "share", m)
		default:
			setScalar(root, "share", v.ShareURL)
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return res, fmt.Errorf("render %s: %w", path, err)
	}
	_ = enc.Close()
	out := buf.Bytes()
	if !isV1 && bytes.Equal(out, original) {
		return res, nil
	}

	tmp := path + ".mason-new"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return res, fmt.Errorf("write %s: %w", tmp, err)
	}
	if _, err := config.Load(config.LoadOptions{UserFile: tmp, NoProject: true}); err != nil {
		_ = os.Remove(tmp)
		return res, fmt.Errorf("refusing to write an invalid %s: %w", path, err)
	}
	if isV1 {
		res.Backup = path + ".v1.bak"
		if err := os.Rename(path, res.Backup); err != nil {
			_ = os.Remove(tmp)
			return res, fmt.Errorf("back up v1 config: %w", err)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return res, fmt.Errorf("write %s: %w", path, err)
	}
	res.Changed = true
	return res, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setScalar(m *yaml.Node, key, value string) {
	if value == "" {
		return
	}
	setNode(m, key, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

func setNode(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			value.LineComment, value.HeadComment, value.FootComment = old.LineComment, old.HeadComment, old.FootComment
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}
