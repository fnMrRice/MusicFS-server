package plugins_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginModulesHaveNoHostImplementationDependencies(t *testing.T) {
	const prefix = "github.com/fnMrRice/MusicFS-server/"
	root := filepath.Join("..", "..", "..")
	for _, tc := range []struct {
		name, directory string
		packages        []string
		forbidden       []string
	}{
		{"host", root, []string{"./src/app/musicfs"}, []string{prefix + "plugins/unified", prefix + "src/online/"}},
		{"sdk", filepath.Join(root, "sdk"), []string{"./..."}, []string{prefix + "src/", prefix + "plugins/"}},
		{"online", filepath.Join(root, "plugins", "source", "online"), []string{"./..."}, []string{prefix + "src/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"list", "-mod=readonly", "-deps"}, tc.packages...)
			cmd := exec.Command("go", args...)
			cmd.Dir = tc.directory
			for _, value := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(value), "GOWORK=") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, "GOWORK=off")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("independent module dependency check: %v\n%s", err, output)
			}
			for _, dependency := range strings.Fields(string(output)) {
				for _, forbidden := range tc.forbidden {
					if strings.HasPrefix(dependency+"/", strings.TrimSuffix(forbidden, "/")+"/") {
						t.Fatalf("%s depends on forbidden implementation %s", tc.name, dependency)
					}
				}
			}
		})
	}
	for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
		t.Run("standalone-"+platform, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-mod=readonly", "-deps", "./cmd/musicfs-plugin-"+platform)
			cmd.Dir = filepath.Join(root, "plugins", "source", "online")
			cmd.Env = append(os.Environ(), "GOWORK=off")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("standalone dependency check: %v\n%s", err, output)
			}
			for _, dependency := range strings.Fields(string(output)) {
				for _, other := range []string{"netease", "qqmusic", "applemusic"} {
					driver := prefix + "plugins/source/online/" + other + "/driver"
					if platform != other && dependency == driver {
						t.Fatalf("%s links another platform driver %s", platform, other)
					}
				}
			}
		})
	}
}
