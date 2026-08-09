package tests

import (
	"github.com/goravel/framework/testing"

	"koi-server/bootstrap"
)

func init() {
	bootstrap.Boot()
}

type TestCase struct {
	testing.TestCase
}
