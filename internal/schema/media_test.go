package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// 用反射给 State 中每个 FileName 字段填入不同的值，确认全部登记在 MediaRefs 中。
// 历史版本的 Settings 是生成时的作品快照，只记录参数，不代表文件仍被使用。
func TestMediaRefsCoverEveryFileName(t *testing.T) {
	var names []string
	var fill func(v reflect.Value, depth int)
	fill = func(v reflect.Value, depth int) {
		if depth > 8 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			v.Set(reflect.New(v.Type().Elem()))
			fill(v.Elem(), depth+1)
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			fill(v.Index(0), depth+1)
		case reflect.Struct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() || (v.Type() == reflect.TypeFor[Generation]() && f.Name == "Settings") {
					continue
				}
				if f.Name == "FileName" && f.Type.Kind() == reflect.String {
					name := fmt.Sprintf("file-%d", len(names))
					names = append(names, name)
					v.Field(i).SetString(name)
					continue
				}
				fill(v.Field(i), depth+1)
			}
		}
	}
	var s State
	fill(reflect.ValueOf(&s).Elem(), 0)
	refs := s.MediaRefs()
	for _, name := range names {
		if !refs["outputs"][name] && !refs["voices"][name] && !refs["instruments"][name] {
			t.Errorf("%s 对应的 FileName 字段未登记在 MediaRefs 中", name)
		}
	}
	if len(names) < 5 {
		t.Fatal("反射未覆盖到预期的 FileName 字段", names)
	}
}

func TestPluginDependenciesKeepManagedSamplesReferenced(t *testing.T) {
	s := State{InstrumentSources: []InstrumentSource{{Plugin: &PluginInstrument{Dependencies: []PluginDependency{{Path: `C:\data\instruments\sample-abc.wav`}, {Path: `C:/data/outputs/import-def.wav`}}}}}}
	refs := s.MediaRefs()
	if !refs["instruments"]["sample-abc.wav"] || !refs["outputs"]["import-def.wav"] {
		t.Fatal("插件仍引用的托管采样不能被清理")
	}
}
