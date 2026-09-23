package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                                         /* 执行当前语句并推进处理流程。 */
	mqttadapter "iot-platform/internal/adapters/mqtt" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"                      /* 执行当前语句并推进处理流程。 */
	"os"                                              /* 执行当前语句并推进处理流程。 */
	"testing"                                         /* 执行当前语句并推进处理流程。 */
	"time"                                            /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestMQTTSharedGatewaySubscription(t *testing.T) { /* 定义 TestMQTTSharedGatewaySubscription 函数。 */
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET") /* 更新 secret 的值。 */
	if broker == "" || secret == "" {                                                          /* 判断条件并选择处理分支。 */
		t.Skip("MQTT integration environment is not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	suffix := randomHex(8)                                    /* 更新 suffix 的值。 */
	tenant := "shared-" + suffix                              /* 更新 tenant 的值。 */
	topic := "/iot/up/" + tenant + "/product/device/property" /* 更新 topic 的值。 */
	manager := auth.New(secret)                               /* 更新 manager 的值。 */
	received := make(chan string, 8)                          /* 更新 received 的值。 */
	clients := []*mqttadapter.Client{}                        /* 更新 clients 的值。 */
	for _, name := range []string{"first", "second"} {        /* 循环处理当前数据。 */
		username := "shared-" + name + "-" + suffix                                                                                                                                                                               /* 更新 username 的值。 */
		token, err := manager.IssueWithACL(username, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}, {Permission: "allow", Action: "publish", Topic: topic}}, time.Minute) /* 检查错误并决定后续处理。 */
		if err != nil {                                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		client, err := mqttadapter.New(broker, username, token, username) /* 更新 err 的值。 */
		if err != nil {                                                   /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer client.Close()                                                         /* 安排函数结束时执行清理。 */
		if err = client.ConfigureSharedSubscriptions("test-" + suffix); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if err = client.SubscribeStandard(func(ctx context.Context, tnt, product, device, kind string, payload []byte) error { /* 判断条件并选择处理分支。 */
			if tnt == tenant { /* 判断条件并选择处理分支。 */
				received <- name /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			return nil /* 返回当前处理结果。 */
		}); err != nil { /* 结束当前表达式或代码块。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		clients = append(clients, client) /* 更新 clients 的值。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)                                                                      /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                               /* 安排函数结束时执行清理。 */
	if err := clients[0].Publish(ctx, topic, []byte(`{"id":"one","timestamp":1788850000000,"data":{"temperature":42}}`), 1, false); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case <-received: /* 处理当前分支。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		t.Fatal("shared subscription did not receive message") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case <-received: /* 处理当前分支。 */
		t.Fatal("one publish was delivered to both gateway workers") /* 验证实际结果符合预期。 */
	case <-time.After(300 * time.Millisecond): /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
