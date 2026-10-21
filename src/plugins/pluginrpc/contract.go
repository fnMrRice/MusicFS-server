package pluginrpc

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Fingerprint pins the complete Go interface and exported DTO field shapes,
// including fields intentionally hidden from the public HTTP JSON contract.
func Fingerprint(contract reflect.Type) string {
	var description strings.Builder
	seen := map[reflect.Type]bool{}
	var visit func(reflect.Type)
	visit = func(t reflect.Type) {
		fmt.Fprintf(&description, "%s/%s:%s;", contractPackagePath(t.PkgPath()), t.Name(), t.Kind())
		if seen[t] || t == contextType || t == errorType {
			return
		}
		seen[t] = true
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			if t.Kind() == reflect.Array {
				fmt.Fprint(&description, t.Len())
			}
			visit(t.Elem())
		case reflect.Map:
			visit(t.Key())
			visit(t.Elem())
		case reflect.Struct:
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				if field.IsExported() {
					fmt.Fprint(&description, field.Name, ";")
					visit(field.Type)
				}
			}
		case reflect.Func:
			for i := 0; i < t.NumIn(); i++ {
				visit(t.In(i))
			}
			fmt.Fprint(&description, "->")
			for i := 0; i < t.NumOut(); i++ {
				visit(t.Out(i))
			}
		case reflect.Interface:
			names := make([]string, t.NumMethod())
			for i := range names {
				names[i] = t.Method(i).Name
			}
			sort.Strings(names)
			for _, name := range names {
				method, _ := t.MethodByName(name)
				fmt.Fprint(&description, name, ";")
				visit(method.Type)
			}
		}
	}
	visit(contract)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(description.String())))
}
