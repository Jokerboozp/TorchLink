package minioadapter /* 声明 minioadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"compress/gzip" /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */

	"github.com/minio/minio-go/v7"                 /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7/pkg/credentials" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Archive struct { /* 定义 Archive 类型。 */
	client  *minio.Client /* 执行当前语句并推进处理流程。 */
	ensured sync.Map      /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(endpoint, access, secret string, tls bool) (*Archive, error) { /* 定义 New 函数。 */
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: tls}) /* 更新 err 的值。 */
	if err != nil {                                                                                                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &Archive{client: c}, nil /* 返回当前处理结果。 */
}                          /* 结束当前表达式或代码块。 */
func safe(v string) string { return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(v) } /* 定义 safe 函数。 */
func (a *Archive) ensure(ctx context.Context, bucket string) error { /* 定义 ensure 函数。 */
	if _, ok := a.ensured.Load(bucket); ok { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ok, err := a.client.BucketExists(ctx, bucket) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !ok { /* 判断条件并选择处理分支。 */
		if err = a.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	a.ensured.Store(bucket, struct{}{}) /* 执行当前语句并推进处理流程。 */
	return nil                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// GetRaw reads objects written by the legacy per-message/batch MinIO archive.
// New ingest traffic is database-backed; this reader remains so old raw
// archive indexes can still be inspected, downloaded and replayed.
func (a *Archive) GetRaw(ctx context.Context, idx model.RawArchiveIndex) (model.RawMessage, error) { /* 定义 GetRaw 函数。 */
	var m model.RawMessage                                                                       /* 声明 m。 */
	o, err := a.client.GetObject(ctx, idx.ObjectBucket, idx.ObjectKey, minio.GetObjectOptions{}) /* 更新 err 的值。 */
	if err != nil {                                                                              /* 判断条件并选择处理分支。 */
		return m, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer o.Close()              /* 安排函数结束时执行清理。 */
	gz, err := gzip.NewReader(o) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return m, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer gz.Close()                     /* 安排函数结束时执行清理。 */
	decoder := json.NewDecoder(gz)       /* 更新 decoder 的值。 */
	for offset := int64(0); ; offset++ { /* 循环处理当前数据。 */
		if err = decoder.Decode(&m); err != nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if offset == idx.ObjectOffset || m.MessageID == idx.MessageID { /* 判断条件并选择处理分支。 */
			return m, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) (string, error) { /* 定义 PutObject 函数。 */
	if err := a.ensure(ctx, bucket); err != nil { /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if size < 0 { /* 判断条件并选择处理分支。 */
		data, err := io.ReadAll(r) /* 更新 err 的值。 */
		if err != nil {            /* 判断条件并选择处理分支。 */
			return "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		r = bytes.NewReader(data) /* 更新 r 的值。 */
		size = int64(len(data))   /* 更新 size 的值。 */
	} /* 结束当前表达式或代码块。 */
	_, err := a.client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType}) /* 更新 err 的值。 */
	return fmt.Sprintf("minio://%s/%s", bucket, key), err                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (a *Archive) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) { /* 定义 GetObject 函数。 */
	return a.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{}) /* 返回当前处理结果。 */
}                                                   /* 结束当前表达式或代码块。 */
func (a *Archive) Health(ctx context.Context) error { _, err := a.client.ListBuckets(ctx); return err } /* 定义 Health 函数。 */
