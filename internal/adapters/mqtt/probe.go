package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"      /* 执行当前语句并推进处理流程。 */
	"crypto/rand"  /* 执行当前语句并推进处理流程。 */
	"encoding/hex" /* 执行当前语句并推进处理流程。 */
	"errors"       /* 执行当前语句并推进处理流程。 */
	"time"         /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Probe verifies current platform credentials with an independent clean session.
// It neither replaces the live subscription client nor publishes device data.
func (c *Client) Probe(ctx context.Context) error { /* 定义 Probe 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if c.broker == "" || c.credentials == nil { /* 判断条件并选择处理分支。 */
		return errors.New("MQTT probe configuration unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var nonce [12]byte                             /* 声明 nonce。 */
	if _, err := rand.Read(nonce[:]); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	probe := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(c.broker).SetProtocolVersion(4).SetClientID("iot-probe-" + hex.EncodeToString(nonce[:])).SetCredentialsProvider(c.credentials).SetCleanSession(true).SetConnectRetry(false).SetAutoReconnect(false).SetConnectTimeout(5 * time.Second)) /* 更新 probe 的值。 */
	defer probe.Disconnect(0)                                                                                                                                                                                                                                                                         /* 安排函数结束时执行清理。 */
	token := probe.Connect()                                                                                                                                                                                                                                                                          /* 更新 token 的值。 */
	select {                                                                                                                                                                                                                                                                                          /* 根据条件选择处理路径。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		return ctx.Err() /* 返回当前处理结果。 */
	case <-token.Done(): /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
	if token.Error() != nil { /* 判断条件并选择处理分支。 */
		if ack, ok := token.(*mqtt.ConnectToken); ok && (ack.ReturnCode() == 4 || ack.ReturnCode() == 5) { /* 判断条件并选择处理分支。 */
			return connector.ErrAuthentication /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return token.Error() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
