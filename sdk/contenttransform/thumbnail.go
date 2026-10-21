// Package contenttransform 定义主进程内内容变换接口，不包含对象权限或发布操作。
package contenttransform

import "context"

const (
	Thumbnail256V1  = "thumbnail256/v1"
	MaxArtworkBytes = 8 << 20
	MaxImagePixels  = 16_000_000
)

// Thumbnailer 接收有界图像字节，输出不放大的 256 像素边界 PNG。
// 变换标识必须随算法语义变化，以便宿主区分派生缓存。
type Thumbnailer interface {
	Transform() string
	Thumbnail(context.Context, []byte) ([]byte, error)
}
