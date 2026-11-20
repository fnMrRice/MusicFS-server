// Command pluginversions 输出官方插件构建所需的 Go 链接参数。
package main

import (
	"fmt"
	"os"

	"github.com/fnMrRice/MusicFS-server/scripts/internal/pluginversions"
)

func main() {
	versions, err := pluginversions.Resolve(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(pluginversions.LinkFlags(versions))
}
