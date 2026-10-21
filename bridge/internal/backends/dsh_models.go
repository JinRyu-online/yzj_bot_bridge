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
	return filepath.Join(resolveDSHHomeDir(dshHome), "settings.yaml")
}

func resolveDSHHomeDir(dshHome string) string {
	home := strings.TrimSpace(dshHome)
	if home != "" {
		return home
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		uh = "."
	}
	return filepath.Join(uh, ".dsh")
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
	return parseDSHModelMap(root)
}

// parseDSHPatchModels 解析 DSH 0.2.x profile 的 cordis.patch.yml：
// 顶层是 patch 条目数组 [ {id, config}, ... ]，把关注的 id 还原成
// settings.yaml 同构 map 后复用 parseDSHModelMap。
func parseDSHPatchModels(data []byte) []ModelInfo {
	var entries []map[string]any
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil
	}
	root := map[string]any{}
	for _, e := range entries {
		id := strings.TrimSpace(fmt.Sprint(e["id"]))
		switch id {
		case "llm-pi-ai", "llm-deepseek", "agent-default-model":
			cfg, _ := e["config"].(map[string]any)
			if cfg != nil {
				root[id] = cfg
			}
		}
	}
	return parseDSHModelMap(root)
}

func parseDSHModelMap(root map[string]any) []ModelInfo {
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
	// map {provider: ..., model: ...}（DSH settings.yaml 实为后者）。
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

// ListDSHModels 汇总当前 DSH 安装可用的模型：
//   - 旧版 CLI：<dsh_home>/settings.yaml（含 .imported 兼容）
//   - 桌面客户端 0.2.x：<dsh_home>/profiles/desktop/cordis.patch.yml
//   - 桥自动生成的 yzj-headless / yzj-sdk profile（可能补过模型）
//
// 按 id 去重、保留首次出现顺序；全部文件都不存在/无模型时返回错误。
func ListDSHModels(dshHome string) ([]ModelInfo, string, error) {
	home := resolveDSHHomeDir(dshHome)
	type cand struct {
		path  string
		patch bool
	}
	cands := []cand{
		{filepath.Join(home, "settings.yaml"), false},
		{filepath.Join(home, "settings.yaml.imported"), false},
		{filepath.Join(home, "profiles", "desktop", "cordis.patch.yml"), true},
		{filepath.Join(home, "profiles", dshManagedHeadlessProfile, "cordis.patch.yml"), true},
		{filepath.Join(home, "profiles", dshManagedSDKProfile, "cordis.patch.yml"), true},
	}
	seen := map[string]struct{}{}
	var out []ModelInfo
	var found []string
	for _, c := range cands {
		data, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		var models []ModelInfo
		if c.patch {
			models = parseDSHPatchModels(data)
		} else {
			models = ParseDSHModels(data)
		}
		if len(models) > 0 {
			found = append(found, c.path)
		}
		for _, m := range models {
			if _, ok := seen[m.ID]; ok {
				continue
			}
			seen[m.ID] = struct{}{}
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("未找到 DSH 模型配置（已查找 %s 与 %s）；请先启动 DSH 桌面客户端并在其中配置模型",
			filepath.Join(home, "settings.yaml"),
			filepath.Join(home, "profiles", "desktop", "cordis.patch.yml"))
	}
	return out, strings.Join(found, "; "), nil
}

// ListDSHModelsFile 读取并解析单个 DSH 配置文件；文件不存在或不可读时返回错误。
// cordis.patch.yml 按 0.2.x patch 数组解析，其余按旧版 settings.yaml 解析。
func ListDSHModelsFile(path string) ([]ModelInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if filepath.Base(path) == "cordis.patch.yml" {
		return parseDSHPatchModels(data), nil
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
