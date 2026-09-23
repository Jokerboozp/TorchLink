package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) LoadAccessState(_ context.Context, tenant string) (model.AccessState, error) { /* 定义 LoadAccessState 函数。 */
	r.mu.RLock()                                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                         /* 安排函数结束时执行清理。 */
	var state model.AccessState                  /* 声明 state。 */
	if b := r.accessStates[tenant]; len(b) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(b, &state); err != nil { /* 判断条件并选择处理分支。 */
			return state, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return state, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAccessState(_ context.Context, tenant string, state model.AccessState) (bool, error) { /* 定义 SaveAccessState 函数。 */
	r.mu.Lock()                                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                          /* 安排函数结束时执行清理。 */
	var old model.AccessState                    /* 声明 old。 */
	if b := r.accessStates[tenant]; len(b) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(b, &old); err != nil { /* 判断条件并选择处理分支。 */
			return false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if old.Revision != state.Revision { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state.Revision++              /* 执行当前语句并推进处理流程。 */
	b, err := json.Marshal(state) /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if r.accessStates == nil { /* 判断条件并选择处理分支。 */
		r.accessStates = map[string][]byte{} /* 更新 r.accessStates 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.accessStates[tenant] = b /* 更新 r.accessStates[tenant] 的值。 */
	return true, nil           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
