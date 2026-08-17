package llm

import "testing"

func TestFeatureConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		fc      FeatureConfig
		wantErr bool
	}{
		{name: "agent", fc: FeatureConfig{Feature: FeatureAgent}, wantErr: false},
		{name: "pref_extract", fc: FeatureConfig{Feature: FeaturePrefExtract}, wantErr: false},
		{name: "subagent", fc: FeatureConfig{Feature: FeatureSubagent}, wantErr: false},
		{name: "unknown", fc: FeatureConfig{Feature: "unknown"}, wantErr: true},
		{name: "empty", fc: FeatureConfig{Feature: ""}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestFeatureModels_Get(t *testing.T) {
	models := &FeatureModels{Configs: map[Feature]FeatureConfig{
		FeatureAgent: {Feature: FeatureAgent, Model: "test-model"},
	}}

	// 存在
	got := models.Get(FeatureAgent)
	if got.Model != "test-model" {
		t.Errorf("Get() = %v, want Model=test-model", got)
	}

	// 不存在
	got = models.Get(FeatureSubagent)
	if got.Model != "" || got.Provider != "" {
		t.Errorf("未配置的 Feature 应返回空，got = %v", got)
	}

	// nil models
	var nilModels *FeatureModels
	got = nilModels.Get(FeatureAgent)
	if got.Model != "" {
		t.Errorf("nil models 应返回空，got = %v", got)
	}
}

func TestFeatureModels_Set(t *testing.T) {
	models := &FeatureModels{}

	// 首次设置
	models.Set(FeatureConfig{Feature: FeatureAgent, Model: "m1"})
	got := models.Get(FeatureAgent)
	if got.Model != "m1" {
		t.Errorf("首次设置后 Get() = %v, want Model=m1", got)
	}

	// 覆盖
	models.Set(FeatureConfig{Feature: FeatureAgent, Model: "m2"})
	got = models.Get(FeatureAgent)
	if got.Model != "m2" {
		t.Errorf("覆盖后 Get() = %v, want Model=m2", got)
	}

	// nil Configs map
	empty := &FeatureModels{}
	empty.Set(FeatureConfig{Feature: FeatureSubagent, Model: "m3"})
	got = empty.Get(FeatureSubagent)
	if got.Model != "m3" {
		t.Errorf("nil map 设置后 Get() = %v, want Model=m3", got)
	}
}
