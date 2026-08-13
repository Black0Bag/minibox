package config

import (
	"reflect"

	"github.com/knadh/koanf/maps"
)

// structProvider 从 Go struct 提供配置（作为默认值来源）。
// 它把 struct 转成扁平化 map，供 koanf 读取。
type structProvider struct {
	Value Config
}

// ReadBytes 不适用（无原始字节）。
func (p structProvider) ReadBytes() ([]byte, error) {
	return nil, nil
}

// Read 返回扁平化的 map[string]any。
func (p structProvider) Read() (map[string]any, error) {
	m := structToMap(p.Value)
	flat, _ := maps.Flatten(m, nil, ".")
	return flat, nil
}

// Watch 不支持（无热更新）。
func (p structProvider) Watch(cb func(event interface{}, err error)) error {
	return nil
}

// structToMap 把 struct 通过反射转成 map[string]any。
// 使用 koanf:"field" 标签作为 key。
func structToMap(v any) map[string]any {
	result := make(map[string]any)
	structToMapInner(reflect.ValueOf(v), result)
	return result
}

func structToMapInner(val reflect.Value, out map[string]any) {
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return
	}
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := field.Tag.Get("koanf")
		if key == "" || key == "-" {
			continue
		}
		fv := val.Field(i)
		switch fv.Kind() {
		case reflect.Struct:
			sub := make(map[string]any)
			structToMapInner(fv, sub)
			out[key] = sub
		case reflect.Ptr:
			if fv.IsNil() {
				continue
			}
			sub := make(map[string]any)
			structToMapInner(fv.Elem(), sub)
			out[key] = sub
		default:
			out[key] = fv.Interface()
		}
	}
}
