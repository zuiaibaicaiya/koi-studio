package audio

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (s *ConfigTestSuite) TestNormalizedFillsSafeDefaults() {
	config := Config{}.normalized()

	s.NotEmpty(config.ModelDir)
	s.Equal("encoder-epoch-99-avg-1.onnx", config.Encoder)
	s.Equal("decoder-epoch-99-avg-1.onnx", config.Decoder)
	s.Equal("joiner-epoch-99-avg-1.onnx", config.Joiner)
	s.Equal("tokens.txt", config.Tokens)
	s.Equal("hotwords.txt", config.HotwordsFile)
	s.Equal(2, config.NumThreads)
	s.Equal(4, config.MaxActivePaths)
	s.Equal(5*time.Second, config.LoadTimeout)
	s.Equal(16000, config.SampleRate)
	s.Equal(80, config.FeatureDim)
	s.Equal(64, config.QueueSize)
	s.Equal(3, config.DecodeBatch)
	s.Equal(200*time.Millisecond, config.EmitInterval)
	s.Equal(20*time.Second, config.MaxUtterance)
	s.Equal(float32(2.0), config.HotwordsScore)
	s.Equal("audio", config.Disk)
}

func (s *ConfigTestSuite) TestNormalizedKeepsExplicitValues() {
	config := Config{
		ModelDir:     "/tmp/model",
		NumThreads:   8,
		SampleRate:   8000,
		QueueSize:    16,
		DecodeBatch:  5,
		EmitInterval: time.Second,
		Disk:         "custom",
	}.normalized()

	s.Equal("/tmp/model", config.ModelDir)
	s.Equal(8, config.NumThreads)
	s.Equal(8000, config.SampleRate)
	s.Equal(16, config.QueueSize)
	s.Equal(5, config.DecodeBatch)
	s.Equal(time.Second, config.EmitInterval)
	s.Equal("custom", config.Disk)
}

func (s *ConfigTestSuite) TestModelPathJoinsModelDir() {
	config := Config{ModelDir: "models/asr"}.normalized()

	s.Equal(filepath.Join("models/asr", "tokens.txt"), config.modelPath(config.Tokens))
}

func (s *ConfigTestSuite) TestDependenciesValidateRequiresAllCollaborators() {
	s.ErrorContains(Dependencies{}.validate(), "log dependency is required")
}
