package broadcasting

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ChannelsTestSuite struct {
	suite.Suite
}

func TestChannelsTestSuite(t *testing.T) {
	suite.Run(t, new(ChannelsTestSuite))
}

func (s *ChannelsTestSuite) TestPrivateChannelNaming() {
	s.Equal("private-client.abc123", PrivateChannel("abc123"))
}

func (s *ChannelsTestSuite) TestAuthorizeAllowsOwnChannel() {
	s.NoError(Authorize("abc123", PrivateChannel("abc123")))
}

func (s *ChannelsTestSuite) TestAuthorizeDeniesOtherClientChannel() {
	s.ErrorIs(Authorize("abc123", PrivateChannel("other")), ErrUnauthorized)
}

func (s *ChannelsTestSuite) TestAuthorizeDeniesChannelWithoutPrefix() {
	s.ErrorIs(Authorize("abc123", "abc123"), ErrUnauthorized)
}

func (s *ChannelsTestSuite) TestAuthorizeDeniesEmptyInput() {
	s.ErrorIs(Authorize("", PrivateChannel("abc123")), ErrUnauthorized)
	s.ErrorIs(Authorize("abc123", ""), ErrUnauthorized)
}
