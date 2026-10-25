package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
)

// everyBinding returns "Group.Field" -> binding for each key binding in km.
func everyBinding(km *huh.KeyMap) map[string]key.Binding {
	out := map[string]key.Binding{}
	var walk func(prefix string, v reflect.Value)
	walk = func(prefix string, v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			name := prefix + v.Type().Field(i).Name
			f := v.Field(i)
			switch f.Type() {
			case reflect.TypeOf(key.Binding{}):
				out[name] = f.Interface().(key.Binding)
			default:
				if f.Kind() == reflect.Struct {
					walk(name+".", f)
				}
			}
		}
	}
	walk("", reflect.ValueOf(km).Elem())
	return out
}

func TestSpanishKeyMap_TranslatesEveryHelp(t *testing.T) {
	english := everyBinding(huh.NewDefaultKeyMap())
	spanish := everyBinding(spanishKeyMap())
	if len(spanish) != len(english) {
		t.Fatalf("binding count differs: %d vs %d", len(spanish), len(english))
	}
	for name, def := range english {
		// FilePicker is not used by MooFetch; Quit has no visible help.
		if strings.HasPrefix(name, "FilePicker.") || def.Help().Desc == "" {
			continue
		}
		if got := spanish[name].Help().Desc; got == "" || got == def.Help().Desc {
			t.Errorf("%s: help %q was not translated", name, got)
		}
	}
}

func TestSpanishKeyMap_KeysStillWork(t *testing.T) {
	def, es := huh.NewDefaultKeyMap(), spanishKeyMap()
	if !reflect.DeepEqual(def.MultiSelect.Toggle.Keys(), es.MultiSelect.Toggle.Keys()) {
		t.Error("toggle keys must not change")
	}
	if !reflect.DeepEqual(def.Select.Filter.Keys(), es.Select.Filter.Keys()) {
		t.Error("filter keys must not change")
	}
	accept := es.Confirm.Accept.Keys()
	for _, k := range []string{"y", "s"} {
		found := false
		for _, a := range accept {
			found = found || a == k
		}
		if !found {
			t.Errorf("confirm accept keys %v must include %q", accept, k)
		}
	}
}

func TestResourceSelectionShowsSpanishHelp(t *testing.T) {
	var picked []string
	f := newForm(huh.NewGroup(huh.NewMultiSelect[string]().
		Options(huh.NewOption("a", "a")).Filterable(true).Value(&picked)))
	f.Init()
	view := f.View()
	for _, want := range []string{"marcar/desmarcar", "filtrar", "marcar todos"} {
		if !strings.Contains(view, want) {
			t.Errorf("form help should contain %q, view:\n%s", want, view)
		}
	}
	for _, bad := range []string{"toggle", "filter", "select all"} {
		if strings.Contains(view, bad) {
			t.Errorf("form help should not contain %q", bad)
		}
	}
}
