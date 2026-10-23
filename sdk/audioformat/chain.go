package audioformat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MaxDecodeDepth 防止损坏容器或插件形成无界解码链。
const MaxDecodeDepth = 8

// ErrUnsupported 只表示本解码器拒绝当前输入，可以尝试下一插件。
// 已识别但损坏、权限不足或实例不可用的错误不能包装该值。
var ErrUnsupported = errors.New("UNSUPPORTED: decoder does not support this input")

// ErrCleanup 表示中间输出仍有资源未清理；宿主必须继续计费并安排重试。
var ErrCleanup = errors.New("TEMPORARY_FAILURE: decoder output cleanup failed")

// Metadata 是解码层之间传递的通用元数据，不携带具体插件的类型。
type Metadata struct {
	Title      string
	Album      string
	Artists    []string
	DurationMs *int64
	BitrateBps *int64
}

type Input struct {
	Reader   io.ReaderAt
	Size     int64
	Format   string
	MIME     string
	Metadata Metadata
}

type Request struct {
	TaskID         string
	AssetID        string
	OutputFormat   string
	MaxOutputBytes int64
}

// Output 的 NeedsDecoding 为 true 时，宿主必须重新识别输出并选择下一层。
// Close 只释放本层创建的资源，不应关闭传入的上一层 Reader。
type Output struct {
	Input
	Attachments   []Attachment
	Processing    []Processing
	NeedsDecoding bool
	Close         func() error
}

type Decoder struct {
	Detector
	// NativePassthrough 声明 native 请求只验证并透传输入，不依赖运行实例。
	NativePassthrough bool
	Decode            func(context.Context, Request, Input) (Output, error)
}

// Processing 由实际执行变换的插件声明，对外描述不由宿主按格式猜测。
type Processing struct{ Type, Version string }

type Chain struct{ decoders []Decoder }

// NewChain 按插件优先级排序；优先级相同时保留注册顺序。
func NewChain(decoders ...Decoder) (*Chain, error) {
	detectors := make([]Detector, 0, len(decoders))
	byID := make(map[string]Decoder, len(decoders))
	for _, decoder := range decoders {
		if decoder.Decode == nil {
			return nil, fmt.Errorf("INVALID_ARGUMENT: decoder callback is required")
		}
		detectors = append(detectors, decoder.Detector)
		byID[decoder.PluginID] = decoder
	}
	registry, err := New(detectors...)
	if err != nil {
		return nil, err
	}
	chain := &Chain{}
	for _, detector := range registry.detectors {
		decoder := byID[detector.PluginID]
		decoder.Detector = detector
		chain.decoders = append(chain.decoders, decoder)
	}
	return chain, nil
}

// Decoded 持有所有中间层；调用方使用完最终 Reader 后必须关闭它。
type Decoded struct {
	Input
	Attachments []Attachment
	Layers      []string
	Processing  []Processing
	closers     []func() error
	mu          sync.Mutex
}

func (d *Decoded) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var failures error
	for index := len(d.closers) - 1; index >= 0; index-- {
		if d.closers[index] == nil {
			continue
		}
		if err := d.closers[index](); err != nil {
			failures = errors.Join(failures, err)
		} else {
			d.closers[index] = nil
		}
	}
	if failures != nil {
		return errors.Join(ErrCleanup, failures)
	}
	return nil
}

// Decode 每层都重新轮询插件，只有显式 ErrUnsupported 才会尝试下一匹配者。
// 最终输出由成功插件的 NeedsDecoding=false 决定，不由宿主猜测格式。
func (c *Chain) Decode(ctx context.Context, request Request, input Input) (_ *Decoded, err error) {
	if c == nil || len(c.decoders) == 0 {
		return nil, ErrUnsupported
	}
	if request.MaxOutputBytes < 0 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid decode output limit")
	}
	result := &Decoded{Input: input}
	defer func() {
		if err != nil {
			err = errors.Join(err, result.Close())
		}
	}()
	seen := make(map[[32]byte]bool)
	for depth := 0; depth < MaxDecodeDepth; depth++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := readHeader(result.Reader, result.Size)
		if err != nil {
			return nil, err
		}
		identity := inputIdentity(header, result.Size)
		if seen[identity] {
			return nil, fmt.Errorf("UNSUPPORTED: decoder chain repeated an input")
		}
		seen[identity] = true
		output, pluginID, err := c.decodeLayer(ctx, request, result.Input, header)
		if err != nil {
			return nil, err
		}
		if output.Close != nil {
			result.closers = append(result.closers, output.Close)
		}
		if output.Reader == nil || output.Size <= 0 || output.Format == "" || output.MIME == "" {
			return nil, fmt.Errorf("INVALID_RESPONSE: decoder returned invalid output")
		}
		if request.MaxOutputBytes > 0 && output.Size > request.MaxOutputBytes {
			return nil, fmt.Errorf("RATE_LIMITED: decoder output exceeds requested limit")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := result.appendAttachments(output.Attachments); err != nil {
			return nil, err
		}
		result.Input = output.Input
		result.Layers = append(result.Layers, pluginID)
		for _, step := range output.Processing {
			if step.Type == "" || len(step.Type) > 128 || step.Version == "" || len(step.Version) > 128 {
				return nil, fmt.Errorf("INVALID_RESPONSE: invalid decoder processing declaration")
			}
			result.Processing = append(result.Processing, step)
		}
		if !output.NeedsDecoding {
			return result, nil
		}
	}
	return nil, fmt.Errorf("UNSUPPORTED: decoder chain exceeded maximum depth")
}

func (c *Chain) decodeLayer(ctx context.Context, request Request, input Input, header []byte) (Output, string, error) {
	for _, decoder := range c.decoders {
		if err := ctx.Err(); err != nil {
			return Output{}, "", err
		}
		match, ok := decoder.Probe(bytes.Clone(header), input.Size)
		if !ok {
			continue
		}
		if match.Format == "" || match.MIME == "" {
			return Output{}, "", fmt.Errorf("INVALID_RESPONSE: invalid decoder probe result")
		}
		candidate := input
		candidate.Format, candidate.MIME = match.Format, match.MIME
		output, err := decoder.Decode(ctx, request, candidate)
		if err != nil {
			if output.Close != nil {
				closeErr := output.Close()
				if closeErr != nil {
					return Output{}, "", errors.Join(err, ErrCleanup, closeErr)
				}
			}
			if errors.Is(err, ErrUnsupported) {
				continue
			}
			return Output{}, "", err
		}
		return output, decoder.PluginID, nil
	}
	return Output{}, "", ErrUnsupported
}

func inputIdentity(header []byte, size int64) [32]byte {
	digest := sha256.New()
	var encodedSize [8]byte
	binary.BigEndian.PutUint64(encodedSize[:], uint64(size))
	digest.Write(encodedSize[:])
	digest.Write(header)
	var result [32]byte
	copy(result[:], digest.Sum(nil))
	return result
}
