package redisadapter /* 声明 redisadapter 包。 */

import "testing" /* 引入当前代码需要的依赖。 */

func TestCacheKeysDoNotCollideWhenIDsContainSeparators(t *testing.T) { /* 定义 TestCacheKeysDoNotCollideWhenIDsContainSeparators 函数。 */
	if stateKey("tenant:a", "device") == stateKey("tenant", "a:device") { /* 判断条件并选择处理分支。 */
		t.Fatal("state cache keys collide") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if latestKey("tenant:a", "device") == latestKey("tenant", "a:device") { /* 判断条件并选择处理分支。 */
		t.Fatal("latest-message cache keys collide") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
