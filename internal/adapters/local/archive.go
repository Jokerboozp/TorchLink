package local /* 声明 local 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"compress/gzip" /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Archive struct{ root string } /* 定义 Archive 类型。 */

func NewArchive(root string) (*Archive, error) { /* 定义 NewArchive 函数。 */
	if err := os.MkdirAll(root, 0o750); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &Archive{root: root}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func safe(v string) string { /* 定义 safe 函数。 */
	v = strings.ReplaceAll(v, "..", "_") /* 更新 v 的值。 */
	v = strings.ReplaceAll(v, "/", "_")  /* 更新 v 的值。 */
	v = strings.ReplaceAll(v, "\\", "_") /* 更新 v 的值。 */
	return v                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) PutRaw(_ context.Context, m model.RawMessage) (model.RawArchiveIndex, error) { /* 定义 PutRaw 函数。 */
	t := time.UnixMilli(m.ReceivedAt).UTC()                                                                                                                                                               /* 更新 t 的值。 */
	bucket := "iot-raw-archive"                                                                                                                                                                           /* 更新 bucket 的值。 */
	key := fmt.Sprintf("%s/%s/%s/%04d/%02d/%02d/%02d/raw-%02d-%s.jsonl.gz", safe(m.TenantID), safe(m.ProductID), safe(m.DeviceID), t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), safe(m.MessageID)) /* 更新 key 的值。 */
	full := filepath.Join(a.root, bucket, filepath.FromSlash(key))                                                                                                                                        /* 更新 full 的值。 */
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {                                                                                                                                        /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) /* 更新 err 的值。 */
	if os.IsExist(err) {                                                  /* 判断条件并选择处理分支。 */
		return a.index(m, bucket, key), nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	gz := gzip.NewWriter(f)                 /* 更新 gz 的值。 */
	encErr := json.NewEncoder(gz).Encode(m) /* 更新 encErr 的值。 */
	closeErr := gz.Close()                  /* 更新 closeErr 的值。 */
	fileErr := f.Close()                    /* 更新 fileErr 的值。 */
	if encErr != nil {                      /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, encErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if closeErr != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, closeErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if fileErr != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, fileErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return a.index(m, bucket, key), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) index(m model.RawMessage, bucket, key string) model.RawArchiveIndex { /* 定义 index 函数。 */
	return model.RawArchiveIndex{MessageID: m.MessageID, TenantID: m.TenantID, ProductID: m.ProductID, DeviceID: m.DeviceID, Protocol: m.Protocol, PayloadFormat: m.PayloadFormat, ObjectBucket: bucket, ObjectKey: key, ObjectOffset: 0, PayloadHash: m.PayloadHash(), PayloadSize: len(m.Payload), ReceivedAt: m.ReceivedAt, ArchivedAt: time.Now().UnixMilli()} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) GetRaw(_ context.Context, idx model.RawArchiveIndex) (model.RawMessage, error) { /* 定义 GetRaw 函数。 */
	var m model.RawMessage                                                                              /* 声明 m。 */
	f, err := os.Open(filepath.Join(a.root, safe(idx.ObjectBucket), filepath.FromSlash(idx.ObjectKey))) /* 更新 err 的值。 */
	if err != nil {                                                                                     /* 判断条件并选择处理分支。 */
		return m, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()              /* 安排函数结束时执行清理。 */
	gz, err := gzip.NewReader(f) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return m, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer gz.Close()                     /* 安排函数结束时执行清理。 */
	err = json.NewDecoder(gz).Decode(&m) /* 更新 err 的值。 */
	return m, err                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) PutObject(_ context.Context, bucket, key string, r io.Reader, _ int64, _ string) (string, error) { /* 定义 PutObject 函数。 */
	full := filepath.Join(a.root, safe(bucket), filepath.FromSlash(key)) /* 更新 full 的值。 */
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {       /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640) /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()                                       /* 安排函数结束时执行清理。 */
	_, err = io.Copy(f, r)                                /* 更新 err 的值。 */
	return fmt.Sprintf("local://%s/%s", bucket, key), err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) { /* 定义 GetObject 函数。 */
	return os.Open(filepath.Join(a.root, safe(bucket), filepath.FromSlash(key))) /* 返回当前处理结果。 */
}                                               /* 结束当前表达式或代码块。 */
func (a *Archive) Health(context.Context) error { _, err := os.Stat(a.root); return err } /* 定义 Health 函数。 */
