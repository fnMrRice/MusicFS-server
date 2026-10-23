package audioruntime

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"io"
)

type Probe func(context.Context, io.ReadSeeker, int64) (audiotypes.Metadata, error)
