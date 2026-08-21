package backends

import (
	"os"
	"path/filepath"
	"testing"
)

const dshSettingsSample = `llm-deepseek:
  models:
    - id: deepseek-v4-flash
      name: DeepSeek V4 Flash
    - id: deepseek-v4
      name: DeepSeek V4
llm-pi-ai:
  providers:
    zeta:
      models:
        - id: qwen3.5-plus
          name: Qwen 3.5 Plus
        - id: deepseek-v4-flash
          name: 重复项应被去重
    kuaidi100:
      models:
        - id: deepseek-r1
agent-default-model: kuaidi100/deepseek-r1
`

// TestParseDSHModels 验证：两段模型收集、按 id 去重、label 优先 name、
// agent-default-model 指定的模型排到最前。
func TestParseDSHModels(t *testing.T) {
	models := ParseDSHModels([]byte(dshSettingsSample))
	if len(models) != 4 {
		t.Fatalf("len=%d want 4: %+v", len(models), models)
	}
	wantIDs := []string{"deepseek-r1", "deepseek-v4-flash", "deepseek-v4", "qwen3.5-plus"}
	for i, id := range wantIDs {
		if models[i].ID != id {
			t.Fatalf("models[%d].ID=%q want %q (all=%+v)", i, models[i].ID, id, models)
		}
	}
	if models[0].Label != "deepseek-r1" {
		t.Fatalf("默认模型无 name 时 label 应回退 id: %q", models[0].Label)
	}
	if models[1].Label != "DeepSeek V4 Flash" {
		t.Fatalf("label=%q want %q", models[1].Label, "DeepSeek V4 Flash")
	}
	if models[3].Label != "Qwen 3.5 Plus" {
		t.Fatalf("label=%q", models[3].Label)
	}
}

func TestParseDSHModelsNoDefault(t *testing.T) {
	models := ParseDSHModels([]byte("llm-deepseek:\n  models:\n    - id: m1\n"))
	if len(models) != 1 || models[0].ID != "m1" || models[0].Label != "m1" {
		t.Fatalf("models=%+v", models)
	}
}

func TestParseDSHModelsInvalidYAML(t *testing.T) {
	if models := ParseDSHModels([]byte(":::not yaml")); models != nil {
		t.Fatalf("invalid yaml should yield nil, got %+v", models)
	}
}

// TestParseDSHModelsDefaultMapForm 验证 agent-default-model 的 map 写法
// （DSH 0.1.0-rc.8 settings.yaml 实为 {provider, model} 结构）同样生效。
func TestParseDSHModelsDefaultMapForm(t *testing.T) {
	sample := `llm-deepseek:
  models:
    - id: deepseek-v4-flash
    - id: deepseek-v4
agent-default-model:
  provider: kuaidi100
  model: deepseek-v4
`
	models := ParseDSHModels([]byte(sample))
	if len(models) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4" {
		t.Fatalf("默认模型（map 写法）应置顶: %+v", models)
	}
}

// TestListDSHModelsFile 验证从临时 settings.yaml 读文件并解析。
func TestListDSHModelsFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(p, []byte(dshSettingsSample), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := ListDSHModelsFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("expected models")
	}
}

// TestListDSHModelsFileMissing 验证文件缺失返回错误。
func TestListDSHModelsFileMissing(t *testing.T) {
	if _, err := ListDSHModelsFile(filepath.Join(t.TempDir(), "no", "settings.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveDSHSettingsPath(t *testing.T) {
	home := filepath.Join("C:", "dsh-home")
	if got := ResolveDSHSettingsPath(home); got != filepath.Join(home, "settings.yaml") {
		t.Fatalf("dsh_home 路径=%q", got)
	}
	got := ResolveDSHSettingsPath("")
	if filepath.Base(filepath.Dir(got)) != ".dsh" || filepath.Base(got) != "settings.yaml" {
		t.Fatalf("默认路径=%q", got)
	}
}
