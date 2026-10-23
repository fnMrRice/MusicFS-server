package audiocodecs

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
)

// 所有消费者使用同一实例准入；后台消费者也持有租约直到输出被释放。
func decodeInstance(ctx context.Context, instance audioruntime.Decoder, decoder audioformat.Decoder, request audioformat.Request, input audioformat.Input, admit Admission) (audioformat.Output, error) {
	if request.OutputFormat == "native" && decoder.NativePassthrough {
		return decoder.Decode(ctx, request, input)
	}
	if admit != nil {
		var err error
		ctx, err = admit(ctx, instance, request, input)
		if err != nil {
			return audioformat.Output{}, err
		}
	}
	lease, err := instance.AcquireLease(ctx)
	if err != nil {
		return audioformat.Output{}, err
	}
	if lease == nil {
		return decoder.Decode(ctx, request, input)
	}
	output, err := decoder.Decode(lease.Context(), request, input)
	if err != nil {
		if output.Close == nil {
			lease.Close()
			return output, err
		}
	}
	if output.Reader != nil {
		output.Reader = admittedReader{lease.Context(), output.Reader}
	}
	for i := range output.Attachments {
		if output.Attachments[i].Reader != nil {
			output.Attachments[i].Reader = admittedReader{lease.Context(), output.Attachments[i].Reader}
		}
	}
	closeOutput := output.Close
	output.Close = func() error {
		if closeOutput != nil {
			if err := closeOutput(); err != nil {
				return err
			}
		}
		lease.Close()
		return nil
	}
	return output, err
}

type admittedReader struct {
	ctx    context.Context
	reader io.ReaderAt
}

func (r admittedReader) ReadAt(data []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.ReadAt(data, offset)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
