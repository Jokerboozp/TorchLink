package redisadapter

import "testing"

func TestCacheKeysDoNotCollideWhenIDsContainSeparators(t *testing.T) {
	if stateKey("tenant:a", "device") == stateKey("tenant", "a:device") {
		t.Fatal("state cache keys collide")
	}
	if latestKey("tenant:a", "device") == latestKey("tenant", "a:device") {
		t.Fatal("latest-message cache keys collide")
	}
}
