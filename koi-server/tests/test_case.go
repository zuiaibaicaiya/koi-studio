package tests

import (
	"os"
	"path/filepath"

	"github.com/goravel/framework/testing"

	"koi-server/bootstrap"
)

func init() {
	// go test 将当前工作目录设置为被测包目录（如 tests/feature/），
	// 而 sherpa-onnx 等 C 库的模型路径（models/speaker/...）是相对于项目根目录的。
	// 此处先将 CWD 切换至项目根，保证 init 阶段引导的框架能正确加载模型。
	chdirToProjectRoot()
	bootstrap.Boot()
}

// chdirToProjectRoot 从当前目录向上查找 go.mod，将工作目录切换到项目根。
func chdirToProjectRoot() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			_ = os.Chdir(dir)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return // 到达文件系统根，放弃
		}
		dir = parent
	}
}

type TestCase struct {
	testing.TestCase
}
