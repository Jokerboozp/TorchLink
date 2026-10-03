package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestMessageTopics(t *testing.T) {
	repositorytest.MessageTopics(t, NewRepository())
}

func TestMessageTopicDevices(t *testing.T) {
	repositorytest.MessageTopicDevices(t, NewRepository())
}
