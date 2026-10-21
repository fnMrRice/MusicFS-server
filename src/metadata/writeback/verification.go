package writeback

import (
	"context"
	"crypto/sha512"
	"fmt"
	"io"
	"os"
)

const maxMetadata = 32 << 20

// audioDigest 由宿主校验插件声明的音频范围并计算摘要，防止只信任重写结果。
func (s *Service) audioDigest(ctx context.Context, file *os.File, size int64) (string, error) {
	audio, err := s.format.AudioRange(file, size)
	if err != nil {
		return "", err
	}
	if audio.Offset < 0 || audio.Size <= 0 || audio.Offset > size || audio.Size > size-audio.Offset {
		return "", invalid("Tag format plugin returned an invalid audio range")
	}
	h := sha512.New()
	n, err := copyContext(ctx, h, io.NewSectionReader(file, audio.Offset, audio.Size))
	if err != nil {
		return "", err
	}
	if n != audio.Size {
		return "", changed()
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (s *Service) verifyAudio(ctx context.Context, file *os.File, size int64, expected string) error {
	actual, err := s.audioDigest(ctx, file, size)
	if err != nil {
		return err
	}
	if actual != expected {
		return changed()
	}
	return nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
}
