package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"           /* 执行当前语句并推进处理流程。 */
	"bytes"           /* 执行当前语句并推进处理流程。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"flag"            /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"io"              /* 执行当前语句并推进处理流程。 */
	"net"             /* 执行当前语句并推进处理流程。 */
	"net/http"        /* 执行当前语句并推进处理流程。 */
	"os"              /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type client struct { /* 定义 client 类型。 */
	baseURL string       /* 执行当前语句并推进处理流程。 */
	token   string       /* 执行当前语句并推进处理流程。 */
	http    *http.Client /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	var ( /* 执行当前语句并推进处理流程。 */
		baseURL  = flag.String("platform", "http://localhost:8081", "platform API base URL")                                                                     /* 更新 baseURL 的值。 */
		gateway  = flag.String("gateway", "", "optional GB26875 TCP gateway address, for example localhost:26875")                                               /* 更新 gateway 的值。 */
		network  = flag.String("network", "tcp", "gateway network: tcp or udp")                                                                                  /* 更新 network 的值。 */
		tenant   = flag.String("tenant", "tenant_001", "tenant ID")                                                                                              /* 更新 tenant 的值。 */
		username = flag.String("username", "admin", "platform username")                                                                                         /* 更新 username 的值。 */
		password = flag.String("password", "", "platform password")                                                                                              /* 更新 password 的值。 */
		deviceID = flag.String("device", "gb26875_virtual_001", "virtual device ID")                                                                             /* 更新 deviceID 的值。 */
		source   = flag.String("source", "123456789012", "12 hexadecimal source-address digits")                                                                 /* 更新 source 的值。 */
		scenario = flag.String("scenario", "manual-alarm", "manual-alarm, manual-normal, smoke-alarm, sound-light-start, sound-light-stop or time-sync-request") /* 更新 scenario 的值。 */
		dryRun   = flag.Bool("dry-run", false, "only print the generated protocol frame")                                                                        /* 更新 dryRun 的值。 */
	) /* 结束当前表达式或代码块。 */
	flag.Parse() /* 执行当前语句并推进处理流程。 */

	sourceBytes, err := hex.DecodeString(strings.TrimSpace(*source)) /* 更新 err 的值。 */
	check(err)                                                       /* 执行当前语句并推进处理流程。 */
	if len(sourceBytes) != 6 {                                       /* 判断条件并选择处理分支。 */
		check(fmt.Errorf("source must contain exactly 12 hexadecimal digits")) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var sourceAddress [6]byte                                                                                                        /* 声明 sourceAddress。 */
	copy(sourceAddress[:], sourceBytes)                                                                                              /* 执行当前语句并推进处理流程。 */
	componentType, status, description, err := scenarioValues(*scenario)                                                             /* 更新 err 的值。 */
	check(err)                                                                                                                       /* 执行当前语句并推进处理流程。 */
	frame := parser.BuildGB26875ComponentStatusFrame(1, sourceAddress, 128, 1, componentType, 1, 1, status, description, time.Now()) /* 更新 frame 的值。 */
	hexFrame := strings.ToUpper(hex.EncodeToString(frame))                                                                           /* 更新 hexFrame 的值。 */
	if *dryRun {                                                                                                                     /* 判断条件并选择处理分支。 */
		fmt.Println(hexFrame) /* 执行当前语句并推进处理流程。 */
		return                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if *gateway != "" { /* 判断条件并选择处理分支。 */
		if *network != "tcp" && *network != "udp" { /* 判断条件并选择处理分支。 */
			check(fmt.Errorf("network must be tcp or udp")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		conn, err := net.DialTimeout(*network, *gateway, 5*time.Second)                    /* 更新 err 的值。 */
		check(err)                                                                         /* 执行当前语句并推进处理流程。 */
		defer conn.Close()                                                                 /* 安排函数结束时执行清理。 */
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))                             /* 更新 _ 的值。 */
		reader := bufio.NewReader(conn)                                                    /* 更新 reader 的值。 */
		registration := parser.BuildGB26875RegistrationFrame(0, sourceAddress, time.Now()) /* 更新 registration 的值。 */
		_, err = conn.Write(registration)                                                  /* 更新 err 的值。 */
		check(err)                                                                         /* 执行当前语句并推进处理流程。 */
		registrationAck, err := readGB26875Frame(reader)                                   /* 更新 err 的值。 */
		check(err)                                                                         /* 执行当前语句并推进处理流程。 */
		check(parseGatewayResponse(registrationAck, "ACK"))                                /* 执行当前语句并推进处理流程。 */
		if *scenario == "time-sync-request" {                                              /* 判断条件并选择处理分支。 */
			request := parser.BuildGB26875TimeSyncRequestFrame(1, sourceAddress, time.Now())                                                                                                /* 更新 request 的值。 */
			_, err = conn.Write(request)                                                                                                                                                    /* 更新 err 的值。 */
			check(err)                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
			response, readErr := readGB26875Frame(reader)                                                                                                                                   /* 更新 readErr 的值。 */
			check(readErr)                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
			check(parseGatewayResponse(response, "TIME_SYNC"))                                                                                                                              /* 执行当前语句并推进处理流程。 */
			fmt.Printf("虚拟设备已通过 %s 网关完成时钟同步请求\n请求帧: %s\n响应帧: %s\n", strings.ToUpper(*network), strings.ToUpper(hex.EncodeToString(request)), strings.ToUpper(hex.EncodeToString(response))) /* 执行当前语句并推进处理流程。 */
			return                                                                                                                                                                          /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_, err = conn.Write(frame)                                                                                                    /* 更新 err 的值。 */
		check(err)                                                                                                                    /* 执行当前语句并推进处理流程。 */
		ack, err := readGB26875Frame(reader)                                                                                          /* 更新 err 的值。 */
		check(err)                                                                                                                    /* 执行当前语句并推进处理流程。 */
		ackMessage := parseGatewayResponseMessage(ack)                                                                                /* 更新 ackMessage 的值。 */
		fmt.Printf("虚拟设备已通过 %s 网关上报 %s，平台返回 %v\n协议帧: %s\n", strings.ToUpper(*network), *scenario, ackMessage.Event["type"], hexFrame) /* 执行当前语句并推进处理流程。 */
		return                                                                                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)                               /* 更新 cancel 的值。 */
	defer cancel()                                                                                         /* 安排函数结束时执行清理。 */
	c := &client{baseURL: strings.TrimRight(*baseURL, "/"), http: &http.Client{Timeout: 10 * time.Second}} /* 更新 c 的值。 */
	var login struct {                                                                                     /* 声明 login。 */
		AccessToken string `json:"accessToken"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	check(c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": *username, "password": *password, "tenantId": *tenant}, &login)) /* 执行当前语句并推进处理流程。 */
	c.token = login.AccessToken                                                                                                                        /* 更新 c.token 的值。 */
	// Keep the operator's published Go release and product binding intact.
	var product model.Product                                                                     /* 声明 product。 */
	check(c.do(ctx, http.MethodGet, "/api/v1/products/product_gb26875_lora_fire", nil, &product)) /* 执行当前语句并推进处理流程。 */
	if product.ProtocolPackageID == "" {                                                          /* 判断条件并选择处理分支。 */
		check(fmt.Errorf("请先上传 Go 协议包并绑定产品，或使用 --gateway 连接通用监听器")) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	check(c.do(ctx, http.MethodPost, "/api/v1/device-registry", map[string]any{ /* 执行当前语句并推进处理流程。 */
		"id": *deviceID, "productId": "product_gb26875_lora_fire", "name": "GB26875 虚拟消防设备", "status": "ENABLED", /* 执行当前语句并推进处理流程。 */
		"deviceRole": "DIRECT", "registrationSource": "VIRTUAL_DEVICE", "tags": map[string]string{"sourceAddress": strings.ToUpper(*source), "protocol": "GB26875"}, /* 执行当前语句并推进处理流程。 */
	}, nil)) /* 结束当前表达式或代码块。 */
	var result map[string]any                                                                       /* 声明 result。 */
	check(c.do(ctx, http.MethodPost, "/api/v1/device-registry/"+*deviceID+"/debug", map[string]any{ /* 执行当前语句并推进处理流程。 */
		"messageId": fmt.Sprintf("raw_gb26875_%d", time.Now().UnixNano()), "payload": hexFrame, /* 执行当前语句并推进处理流程。 */
	}, &result)) /* 结束当前表达式或代码块。 */
	encoded, _ := json.MarshalIndent(result, "", "  ")                                           /* 更新 _ 的值。 */
	fmt.Printf("虚拟设备 %s 已上报场景 %s\n协议帧: %s\n平台响应: %s\n", *deviceID, *scenario, hexFrame, encoded) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func scenarioValues(name string) (byte, uint16, string, error) { /* 定义 scenarioValues 函数。 */
	switch name { /* 根据条件选择处理路径。 */
	case "manual-alarm": /* 处理当前分支。 */
		return 23, 1 << 1, "virtual manual alarm", nil /* 返回当前处理结果。 */
	case "manual-normal": /* 处理当前分支。 */
		return 23, 0, "virtual manual normal", nil /* 返回当前处理结果。 */
	case "smoke-alarm": /* 处理当前分支。 */
		return 40, 1 << 1, "virtual smoke alarm", nil /* 返回当前处理结果。 */
	case "sound-light-start": /* 处理当前分支。 */
		return 137, 1<<5 | 1<<6, "virtual sound light started", nil /* 返回当前处理结果。 */
	case "sound-light-stop": /* 处理当前分支。 */
		return 137, 0, "virtual sound light stopped", nil /* 返回当前处理结果。 */
	case "time-sync-request": /* 处理当前分支。 */
		return 0, 0, "virtual time synchronization request", nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0, 0, "", fmt.Errorf("unknown scenario %q", name) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func readGB26875Frame(reader *bufio.Reader) ([]byte, error) { /* 定义 readGB26875Frame 函数。 */
	for { /* 循环处理当前数据。 */
		first, err := reader.ReadByte() /* 更新 err 的值。 */
		if err != nil {                 /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if first != '@' { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		second, err := reader.ReadByte() /* 更新 err 的值。 */
		if err != nil {                  /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if second != '@' { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		break /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	header := make([]byte, 25)                             /* 更新 header 的值。 */
	if _, err := io.ReadFull(reader, header); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	length := int(binary.LittleEndian.Uint16(header[22:24])) /* 更新 length 的值。 */
	if length > 512 {                                        /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("application data length %d exceeds 512", length) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tail := make([]byte, length+3)                       /* 更新 tail 的值。 */
	if _, err := io.ReadFull(reader, tail); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return append(append([]byte{'@', '@'}, header...), tail...), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func parseGatewayResponse(frame []byte, expected string) error { /* 定义 parseGatewayResponse 函数。 */
	message := parseGatewayResponseMessage(frame)                  /* 更新 message 的值。 */
	if got, _ := message.Event["type"].(string); got != expected { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("gateway response event %q, want %q", got, expected) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func parseGatewayResponseMessage(frame []byte) *model.StandardMessage { /* 定义 parseGatewayResponseMessage 函数。 */
	payload, _ := json.Marshal(hex.EncodeToString(frame))                                                                                                                                               /* 更新 _ 的值。 */
	message, err := (parser.GB26875Parser{}).Parse(model.RawMessage{MessageID: "raw_ack", Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload, ReceivedAt: time.Now().UnixMilli()}) /* 更新 err 的值。 */
	check(err)                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	return message                                                                                                                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c *client) do(ctx context.Context, method, path string, body, out any) error { /* 定义 do 函数。 */
	payload, err := json.Marshal(body) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	if c.token != "" {                                 /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+c.token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := c.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                           /* 安排函数结束时执行清理。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("%s %s returned %s: %s", method, path, resp.Status, strings.TrimSpace(string(responseBody))) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if out != nil && len(responseBody) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(responseBody, out); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func check(err error) { /* 定义 check 函数。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		fmt.Fprintln(os.Stderr, "错误:", err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                          /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
