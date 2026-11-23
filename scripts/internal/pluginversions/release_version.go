package pluginversions

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

const serverVersionPath = "src/core/version/version.go"

// 核对发布提交树中的产品版本，避免错误标题改变后续插件的计数基点。
func (r repository) verifyReleaseVersion(revision, expected string) error {
	data, err := r.git("show", revision+":"+serverVersionPath)
	if err != nil {
		return fmt.Errorf("read server version at release %s: %w", revision, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), serverVersionPath, data, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if name.Name != "Server" || i >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if ok && literal.Kind == token.STRING {
					actual, err := strconv.Unquote(literal.Value)
					if err == nil && actual == expected {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("release title at %s requires const Server = %q in %s", revision, expected, serverVersionPath)
}
