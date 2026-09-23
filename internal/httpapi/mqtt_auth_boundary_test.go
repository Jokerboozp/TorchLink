package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"os"      /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"               /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// This probe does not publish or subscribe to any business topic.
func TestMQTTBrokerRejectsInvalidJWT(t *testing.T) { /* 定义 TestMQTTBrokerRejectsInvalidJWT 函数。 */
	broker := os.Getenv("IOT_TEST_MQTT_BROKER") /* 更新 broker 的值。 */
	if broker == "" {                           /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_MQTT_BROKER not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	id := "auth-probe-" + randomHex(8)                                                                                                                                                                                                 /* 更新 id 的值。 */
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id).SetPassword("invalid-jwt").SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second)) /* 更新 client 的值。 */
	defer client.Disconnect(100)                                                                                                                                                                                                       /* 安排函数结束时执行清理。 */
	token := client.Connect()                                                                                                                                                                                                          /* 更新 token 的值。 */
	if !token.WaitTimeout(2 * time.Second) {                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("broker did not return an authentication verdict") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	code := token.(*mqtt.ConnectToken).ReturnCode()       /* 更新 code 的值。 */
	if token.Error() == nil || (code != 4 && code != 5) { /* 判断条件并选择处理分支。 */
		t.Fatalf("invalid JWT was not rejected: CONNACK %d", code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMQTTBrokerBindsJWTUsername(t *testing.T) { /* 定义 TestMQTTBrokerBindsJWTUsername 函数。 */
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET") /* 更新 secret 的值。 */
	if broker == "" || secret == "" {                                                          /* 判断条件并选择处理分支。 */
		t.Skip("MQTT integration environment not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	id := "identity-probe-" + randomHex(8)                                             /* 更新 id 的值。 */
	jwt, err := auth.New(secret).IssueWithACL(id, id, "device", nil, nil, time.Minute) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal("could not issue probe JWT") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id + "-wrong").SetPassword(jwt).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second)) /* 更新 client 的值。 */
	defer client.Disconnect(100)                                                                                                                                                                                                        /* 安排函数结束时执行清理。 */
	token := client.Connect()                                                                                                                                                                                                           /* 更新 token 的值。 */
	if !token.WaitTimeout(2 * time.Second) {                                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("broker did not return an authentication verdict") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	code := token.(*mqtt.ConnectToken).ReturnCode()       /* 更新 code 的值。 */
	if token.Error() == nil || (code != 4 && code != 5) { /* 判断条件并选择处理分支。 */
		t.Fatalf("JWT username mismatch was not rejected: CONNACK %d", code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
