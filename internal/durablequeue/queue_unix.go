//go:build !windows

package durablequeue /* 声明 durablequeue 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"golang.org/x/sys/unix" /* 执行当前语句并推进处理流程。 */
	"os"                    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func lockQueue(path string) (*os.File, error) { /* 定义 lockQueue 函数。 */
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { /* 判断条件并选择处理分支。 */
		f.Close()       /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return f, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func syncDirectory(path string) error { /* 定义 syncDirectory 函数。 */
	f, err := os.Open(path) /* 更新 err 的值。 */
	if err != nil {         /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close() /* 安排函数结束时执行清理。 */
	return f.Sync() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
