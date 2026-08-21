package backends

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolveDSHSettingsPath 返回 DSH settings.yaml 路径：dshHome 非空时
// <dshHome>/settings.yaml，否则 ~/.dsh/settings.yaml（os.UserHomeDir 拼 .dsh）。
func ResolveDSHSettingsPath(dshHome string) string {
	home := strings.TrimSpace(dshHome)
	if home == "" {
		uh, err := os.UserHomeDir()
		if err != nil {
			uh = "."
		}
		home = filepath.Join(uh, ".dsh")
	}
	return filepath.Join(home, "settings.yaml")
}

// ParseDSHModels 解析 DSH settings.yaml 内容，收集模型列表：
// llm-deepseek.models[] + llm-pi-ai.providers.*.models[]（每项 id、可选 name）。
// 按 id 去重（首次出现优先），label 优先 name 否则 id；
// 顶层 agent-default-model 的 "provider/model" 指定模型排到最前。
func ParseDSHModels(data []byte) []ModelInfo {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ModelInfo
	add := func(id, name string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(name)
		if label == "" {
			label = id
		}
		out = append(out, ModelInfo{ID: id, Label: label})
	}

	if sec, ok := root["llm-deepseek"].(map[string]any); ok {
		if models, ok := sec["models"].([]any); ok {
			for _, m := range models {
				if mm, ok := m.(map[string]any); ok {
					add(yamlString(mm["id"]), yamlString(mm["name"]))
				}
			}
		}
	}

	// providers 键顺序随机，排序后遍历保证输出确定性。
	if sec, ok := root["llm-pi-ai"].(map[string]any); ok {
		if provs, ok := sec["providers"].(map[string]any); ok {
			keys := make([]string, 0, len(provs))
			for k := range provs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				pv, ok := provs[k].(map[string]any)
				if !ok {
					continue
				}
				models, ok := pv["models"].([]any)
				if !ok {
					continue
				}
				for _, m := range models {
					if mm, ok := m.(map[string]any); ok {
						add(yamlString(mm["id"]), yamlString(mm["name"]))
					}
				}
			}
		}
	}

	// agent-default-model 支持两种写法：字符串 "provider/model"，或
	// map {provider: ..., model: ...}（DSH 0.1.0-rc.8 settings.yaml 实为后者）。
	// 解析出 model 后把它排到最前。
	defaultModel := ""
	if def, ok := root["agent-default-model"].(string); ok {
		if _, model, found := strings.Cut(strings.TrimSpace(def), "/"); found {
			defaultModel = strings.TrimSpace(model)
		}
	} else if def, ok := root["agent-default-model"].(map[string]any); ok {
		if model, ok := def["model"].(string); ok {
			defaultModel = strings.TrimSpace(model)
		}
	}
	if defaultModel != "" {
		for i, m := range out {
			if m.ID == defaultModel {
				if i > 0 {
					out = append(out[:i], out[i+1:]...)
					out = append([]ModelInfo{m}, out...)
				}
				return out
			}
		}
		// 默认模型不在收集列表中：直接补到最前。
		out = append([]ModelInfo{{ID: defaultModel, Label: defaultModel}}, out...)
	}
	return out
}

// ListDSHModelsFile 读取并解析 DSH settings.yaml；文件不存在或不可读时返回错误。
func ListDSHModelsFile(path string) ([]ModelInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDSHModels(data), nil
}

// yamlString 把 yaml.v3 标量值转成字符串（nil → ""）。
func yamlString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
