package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func StandardTopic(topic string) (tenant, product, device, kind string, err error) { /* 定义 StandardTopic 函数。 */
	p := strings.Split(topic, "/")                                                                                                                           /* 更新 p 的值。 */
	if len(p) != 7 || p[0] != "" || p[1] != "iot" || p[2] != "up" || (p[6] != "property" && p[6] != "event" && p[6] != "state" && p[6] != "command-reply") { /* 判断条件并选择处理分支。 */
		err = errors.New("expected /iot/up/{tenant}/{product}/{device}/{property|event|state|command-reply}") /* 更新 err 的值。 */
		return                                                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return p[3], p[4], p[5], p[6], nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Broker authentication/ACL establishes publisher identity. The handler also
// checks the current inventory status and never accepts identity in the body.
func (c *Client) SubscribeStandard(handler func(context.Context, string, string, string, string, []byte) error) error { /* 定义 SubscribeStandard 函数。 */
	return c.register("standard", []string{"/iot/up/+/+/+/+"}, func(ctx context.Context, topic string, payload []byte) error { /* 返回当前处理结果。 */
		tenant, product, device, kind, err := StandardTopic(topic) /* 更新 err 的值。 */
		if err != nil || len(payload) > 64<<10 {                   /* 判断条件并选择处理分支。 */
			return Reject(errors.New("invalid standard topic or payload")) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return handler(ctx, tenant, product, device, kind, payload) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
