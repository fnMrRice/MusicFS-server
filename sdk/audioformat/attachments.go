package audioformat

import (
	"fmt"
	"io"
)

// MaxAttachments 限制整条解码链持有的容器附件数量。
const MaxAttachments = 32

// Attachment 是容器中独立于音频的原始资源，不把正文放进元数据。
// Reader 从附件首字节开始，生命周期由所属解码层的 Close 管理。
// 图片格式、内容和可用大小由消费附件的刮削插件另行校验。
type Attachment struct {
	Kind   string
	Reader io.ReaderAt
	Size   int64
}

func (d *Decoded) appendAttachments(items []Attachment) error {
	if len(items) > MaxAttachments-len(d.Attachments) {
		return fmt.Errorf("RATE_LIMITED: decoder attachment count exceeds limit")
	}
	for _, item := range items {
		if item.Kind != "artwork" || item.Reader == nil || item.Size <= 0 || item.Size > 8<<40 {
			return fmt.Errorf("INVALID_RESPONSE: decoder returned invalid attachment")
		}
	}
	d.Attachments = append(d.Attachments, items...)
	return nil
}
