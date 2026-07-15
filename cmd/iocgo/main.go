package main

import (
	"chatgpt-adapter/cmd/iocgo/annotation"
	"github.com/iocgo/sdk/gen"
	"github.com/iocgo/sdk/gen/tool"
	"os"
	"path/filepath"
	"runtime"
)

//
// env:  GOTOOLDIR=tool
// argv: tool/compile.exe -d.log debug -p chatgpt-adapter /home/xxx/chatgpt-adapter/relay/alloc/coze/coze.go
//

func init() {
	// gin
	gen.Alias[annotation.GET]()
	gen.Alias[annotation.PUT]()
	gen.Alias[annotation.DEL]()
	gen.Alias[annotation.POST]()

	// cobra
	gen.Alias[annotation.Cobra]()
}

func main() {
	// The annotation processor invokes `go list` while running as a toolexec
	// child. Some Go/Windows combinations narrow PATH for tool children, so make
	// the host Go binary explicitly discoverable before processing annotations.
	goBin := filepath.Join(runtime.GOROOT(), "bin")
	_ = os.Setenv("PATH", goBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool.Process()
}
