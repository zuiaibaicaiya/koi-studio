package offlinetranscribe

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ConcurrencyConfigTestSuite struct {
	suite.Suite
}

func TestConcurrencyConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConcurrencyConfigTestSuite))
}

// 未显式配置时按 CPU 核数自动推算并发度，并夹在 [2, maxAutoConcurrency] 区间内。
func (s *ConcurrencyConfigTestSuite) TestMaxConcurrencyAuto() {
	got := Config{}.normalized().MaxConcurrency
	want := defaultMaxConcurrency()
	s.Equal(want, got)
	s.GreaterOrEqual(got, 2)
	s.LessOrEqual(got, maxAutoConcurrency)

	if n := runtime.NumCPU(); n >= 2 && n <= maxAutoConcurrency {
		s.Equal(n, got, "核数在区间内时应直接取核数")
	}
}

// 显式配置的并发度被保留；超过上限时收敛到上限（防止内存占用失控）。
func (s *ConcurrencyConfigTestSuite) TestMaxConcurrencyExplicit() {
	s.Equal(1, Config{MaxConcurrency: 1}.normalized().MaxConcurrency)
	s.Equal(3, Config{MaxConcurrency: 3}.normalized().MaxConcurrency)
	s.Equal(maxAutoConcurrency, Config{MaxConcurrency: maxAutoConcurrency * 10}.normalized().MaxConcurrency)
	// 负数/0 视为未配置。
	s.Equal(defaultMaxConcurrency(), Config{MaxConcurrency: -1}.normalized().MaxConcurrency)
}
